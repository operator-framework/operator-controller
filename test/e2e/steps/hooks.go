package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
	"github.com/go-logr/logr"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/component-base/featuregate"
	"k8s.io/klog/v2/textlogger"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

type resource struct {
	name      string
	kind      string
	namespace string
}

// deploymentRestore records the original state of a deployment so it can be
// rolled back after a test that modifies deployment configuration.
type deploymentRestore struct {
	name          string // deployment name
	namespace     string
	containerName string   // container to patch (for env var restores)
	patchedArgs   bool     // true when container args were modified (for TLS profile patches)
	originalArgs  []string // original container args; may be nil if args were unset
	originalEnv   []string // original env vars as "NAME=VALUE" (for proxy patches)
}

type scenarioContext struct {
	id                   string
	featureName          string
	scenarioName         string
	namespace            string
	clusterExtensionName string
	clusterObjectSetName string
	catalogs             map[string]string // user-chosen name -> ClusterCatalog resource name
	catalogPackageNames  map[string]string // original package name -> parameterized name
	addedResources       []resource
	removedResources     []unstructured.Unstructured
	metricsResponse      map[string]string
	leaderPods           map[string]string // component name -> leader pod name
	deploymentRestores   []deploymentRestore
	extensionObjects     []client.Object
	proxy                *recordingProxy
	catalogAddr          string
	catalogCleanup       func()
	lastApplyStderr      string
}

// GatherClusterExtensionObjects collects all resources related to the ClusterExtension container in
// either their Helm release Secret or ClusterObjectSet depending on the applier being used
// and saves them into the context.
func (s *scenarioContext) GatherClusterExtensionObjects() error {
	objs, err := listExtensionResources(s.clusterExtensionName)
	if err != nil {
		return fmt.Errorf("failed to load extension resources into context: %w", err)
	}
	s.extensionObjects = objs
	return nil
}

// GetClusterExtensionObjects returns the ClusterExtension objects currently saved into the context.
// Will always return nil until GatherClusterExtensionObjects is called
func (s *scenarioContext) GetClusterExtensionObjects() []client.Object {
	return s.extensionObjects
}

type contextKey string

const (
	scenarioContextKey contextKey = "scenario-context"
)

var (
	devMode              = false
	objectControllerOnly bool
	featureGates         = map[featuregate.Feature]bool{
		features.WebhookProviderCertManager:        true,
		features.SingleOwnNamespaceInstallSupport:  false,
		features.WebhookProviderOpenshiftServiceCA: false,
		features.BoxcutterRuntime:                  false,
		features.DeploymentConfig:                  false,
		catalogdHAFeature:                          false,
		objectControllerFeature:                    false,
	}
	logger logr.Logger
)

func init() {
	flagSet := pflag.CommandLine
	flagSet.BoolVar(&devMode, "log.debug", false, "print debug log level")
	flagSet.BoolVar(&objectControllerOnly, "e2e.object-controller-only", false, "require object-controller with no ClusterExtension or ClusterCatalog APIs for direct ClusterObjectSet scenarios")
}

func RegisterHooks(sc *godog.ScenarioContext) {
	sc.Before(CheckFeatureTags)
	sc.Before(CreateScenarioContext)
	RegisterAsciiCastHooks(sc)

	sc.After(ScenarioCleanup)
}

// detectOLMDeployments finds installed components across namespaces. Missing
// components are valid for standalone installations and upgrade scenarios.
func detectOLMDeployments() (map[string]*appsv1.Deployment, error) {
	raw, err := k8sClient(context.Background(), "get", "deployments", "-A", "-l", "app.kubernetes.io/part-of=olm", "-o", "jsonpath={.items}")
	if err != nil {
		return nil, err
	}
	dl := []appsv1.Deployment{}
	if err := json.Unmarshal([]byte(raw), &dl); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OLM deployments: %v", err)
	}

	deployments := map[string]*appsv1.Deployment{}
	for i := range dl {
		switch dl[i].Name {
		case olmDeploymentName:
			deployments["operator-controller"] = &dl[i]
		case catalogdDeploymentName:
			deployments["catalogd"] = &dl[i]
		case objectControllerDeploymentName:
			deployments["object-controller"] = &dl[i]
		}
	}
	return deployments, nil
}

func BeforeSuite() {
	if devMode {
		logger = textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(1)))
	} else {
		logger = textlogger.NewLogger(textlogger.NewConfig())
	}

	// Enable HA scenarios when the cluster has at least 2 nodes.  This runs
	// unconditionally so that upgrade scenarios (which install OLM in a Background
	// step and return early below) still get the gate set correctly.
	if out, err := k8sClient(context.Background(), "get", "nodes", "--no-headers", "-o", "name"); err == nil &&
		len(strings.Fields(strings.TrimSpace(out))) >= 2 {
		featureGates[catalogdHAFeature] = true
	}

	deployments, err := detectOLMDeployments()
	if err != nil {
		logger.Info("OLM deployments not found; skipping feature gate detection (upgrade scenarios will install OLM in Background)")
		return
	}
	configureControllerFeatures(deployments)
}

func configureControllerFeatures(deployments map[string]*appsv1.Deployment) {
	objectController := deployments["object-controller"]
	featureGates[objectControllerFeature] = objectController != nil
	if objectController != nil {
		componentNamespaces["object-controller"] = objectController.Namespace
	}
	olm, catalogdDep := deployments["operator-controller"], deployments["catalogd"]
	if olm == nil {
		logger.Info("operator-controller is absent; using independently detected components")
		return
	}
	olmNamespace = olm.Namespace
	componentNamespaces["operator-controller"] = olmNamespace

	// Catalogd may be in a different namespace than operator-controller.
	catalogdNS := olmNamespace
	if catalogdDep != nil {
		catalogdNS = catalogdDep.Namespace
	}
	componentNamespaces["catalogd"] = catalogdNS

	// Refine CatalogdHA based on actual catalogd replica count now that catalogdNS is
	// known.  The node-count check above can fire on any multi-node cluster even when
	// catalogd runs with only 1 replica.  Override the gate: HA scenarios require ≥2
	// catalogd replicas.  Fall back to whatever the node-count check set when catalogd
	// was not found or the replica count is not parseable.
	if catalogdDep != nil {
		if replicas := catalogdDep.Spec.Replicas; replicas != nil {
			featureGates[catalogdHAFeature] = *replicas >= 2
		}
	}

	featureGatePattern := regexp.MustCompile(`--feature-gates=([[:alnum:]]+)=(true|false)`)
	for _, c := range olm.Spec.Template.Spec.Containers {
		if c.Name == "manager" {
			for _, arg := range c.Args {
				if matches := featureGatePattern.FindStringSubmatch(arg); matches != nil {
					v, err := strconv.ParseBool(matches[2])
					if err != nil {
						panic(fmt.Errorf("failed to parse feature gate %q=%q: %v", matches[1], matches[2], err))
					}
					featureGates[featuregate.Feature(matches[1])] = v
				}
			}
		}
	}

	logger.Info(fmt.Sprintf("Enabled feature gates: %v", featureGates))
}

func CheckFeatureTags(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	for _, tag := range sc.Tags {
		if enabled, found := featureGates[featuregate.Feature(tag.Name[1:])]; found && !enabled {
			// An explicitly requested standalone suite, or an integrated Boxcutter
			// installation, must fail readiness if its controller is missing.
			if tag.Name == "@"+string(objectControllerFeature) && (objectControllerOnly || featureGates[features.BoxcutterRuntime]) {
				continue
			}
			logger.Info(fmt.Sprintf("Skipping scenario %q because feature gate %q is disabled", sc.Name, tag.Name[1:]))
			return ctx, godog.ErrSkip
		}
	}
	return ctx, nil
}

func CreateScenarioContext(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	scCtx := &scenarioContext{
		id:                   sc.Id,
		featureName:          strings.TrimSuffix(filepath.Base(sc.Uri), filepath.Ext(sc.Uri)),
		scenarioName:         sc.Name,
		namespace:            fmt.Sprintf("ns-%s", sc.Id),
		clusterExtensionName: fmt.Sprintf("ce-%s", sc.Id),
		clusterObjectSetName: fmt.Sprintf("cos-%s", sc.Id),
		catalogs:             make(map[string]string),
		catalogPackageNames:  make(map[string]string),
		metricsResponse:      make(map[string]string),
		leaderPods:           make(map[string]string),
	}
	return context.WithValue(ctx, scenarioContextKey, scCtx), nil
}

func scenarioCtx(ctx context.Context) *scenarioContext {
	return ctx.Value(scenarioContextKey).(*scenarioContext)
}

func stderrOutput(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr != nil {
		return string(exitErr.Stderr)
	}
	return ""
}

func ScenarioCleanup(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
	sc := scenarioCtx(ctx)
	// Stop the in-process recording proxy if one was started.
	if sc.proxy != nil {
		sc.proxy.stop()
	}
	// Stop catalog port-forward if one was started.
	if sc.catalogCleanup != nil {
		sc.catalogCleanup()
	}

	// Restore any deployments that were modified during the scenario.  Runs
	// unconditionally (even on failure) to prevent a misconfigured deployment
	// from bleeding into subsequent scenarios.  Restored in LIFO order so that
	// multiple patches to the same deployment unwind to the true original.
	for i := len(sc.deploymentRestores) - 1; i >= 0; i-- {
		dr := sc.deploymentRestores[i]
		if dr.patchedArgs {
			if err2 := patchDeploymentArgs(dr.namespace, dr.name, dr.originalArgs); err2 != nil {
				logger.Info("Error restoring deployment args", "name", dr.name, "error", err2)
			} else if _, err2 := k8sClient(ctx, "rollout", "status", "-n", dr.namespace,
				fmt.Sprintf("deployment/%s", dr.name), "--timeout=2m"); err2 != nil {
				logger.Info("Timeout waiting for deployment rollout after restore", "name", dr.name)
			}
		}
		if err2 := restoreDeployment(dr); err2 != nil {
			logger.Info("Error restoring deployment env", "deployment", dr.name, "namespace", dr.namespace, "error", err2)
		}
	}

	if err != nil {
		return ctx, err
	}

	forDeletion := sc.addedResources
	for _, catalogName := range sc.catalogs {
		forDeletion = append(forDeletion, resource{name: catalogName, kind: "clustercatalog"})
	}
	forDeletion = append(forDeletion, resource{name: sc.namespace, kind: "namespace"})

	g := new(errgroup.Group)
	g.SetLimit(8)
	for _, r := range forDeletion {
		g.Go(func() error {
			args := []string{"delete", r.kind, r.name, "--ignore-not-found=true"}
			if r.namespace != "" {
				args = append(args, "-n", r.namespace)
			}
			if _, err := k8sClient(ctx, args...); err != nil {
				logger.Info("Error deleting resource", "name", r.name, "namespace", r.namespace, "stderr", stderrOutput(err))
			}
			return nil
		})
	}
	_ = g.Wait()
	return ctx, nil
}
