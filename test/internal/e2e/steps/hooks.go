package steps

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/go-logr/logr"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/component-base/featuregate"
	"k8s.io/klog/v2/textlogger"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

type Resource struct {
	Name      string
	Kind      string
	Namespace string
}

// deploymentRestore records the original state of a deployment so it can be
// rolled back after a test that modifies deployment configuration.
type deploymentRestore struct {
	Name          string // deployment name
	Namespace     string
	containerName string   // container to patch (for env var restores)
	patchedArgs   bool     // true when container args were modified (for TLS profile patches)
	originalArgs  []string // original container args; may be nil if args were unset
	originalEnv   []string // original env vars as "NAME=VALUE" (for proxy patches)
}

// World holds scenario state shared by component step packages.
type World struct {
	ID                   string
	FeatureName          string
	ScenarioName         string
	Namespace            string
	ClusterExtensionName string
	ClusterObjectSetName string
	Catalogs             map[string]string // user-chosen name -> ClusterCatalog resource name
	CatalogPackageNames  map[string]string // original package name -> parameterized name
	AddedResources       []Resource
	RemovedResources     []unstructured.Unstructured
	MetricsResponse      map[string]string
	LeaderPods           map[string]string // component name -> leader pod name
	deploymentRestores   []deploymentRestore
	ExtensionObjects     []client.Object
	RevisionSecrets      []Resource
	ResourceUIDs         map[string]types.UID
	proxy                *recordingProxy
	CatalogAddr          string
	CatalogCleanup       func()
	LastApplyStderr      string
}

// GatherClusterExtensionObjects collects all resources related to the ClusterExtension container in
// either their Helm release Secret or ClusterObjectSet depending on the applier being used
// and saves them into the context.
func (s *World) GatherClusterExtensionObjects() error {
	objs, err := listExtensionResources(s.ClusterExtensionName)
	if err != nil {
		return fmt.Errorf("failed to load extension resources into context: %w", err)
	}
	s.ExtensionObjects = objs
	return nil
}

// GetClusterExtensionObjects returns the ClusterExtension objects currently saved into the context.
// Will always return nil until GatherClusterExtensionObjects is called
func (s *World) GetClusterExtensionObjects() []client.Object {
	return s.ExtensionObjects
}

type contextKey string

const (
	scenarioContextKey contextKey = "scenario-context"
)

var (
	devMode      = false
	FeatureGates = map[featuregate.Feature]bool{
		features.WebhookProviderCertManager:        true,
		features.SingleOwnNamespaceInstallSupport:  false,
		features.WebhookProviderOpenshiftServiceCA: false,
		features.BoxcutterRuntime:                  false,
		features.DeploymentConfig:                  false,
		catalogdHAFeature:                          false,
	}
	logger logr.Logger
)

func init() {
	flagSet := pflag.CommandLine
	flagSet.BoolVar(&devMode, "log.debug", false, "print debug log level")
}

func RegisterHooks(sc *godog.ScenarioContext) {
	sc.Before(CheckFeatureTags)
	sc.Before(CreateScenarioContext)
	RegisterAsciiCastHooks(sc)

	sc.After(ScenarioCleanup)
}

// detectOLMDeployments returns the operator-controller deployment (first) and the catalogd
// deployment (second) found via the app.kubernetes.io/part-of=olm label across all namespaces.
// The catalogd return value may be nil when OLM is not yet installed (upgrade scenarios
// install it in a Background step).
func detectOLMDeployments() (*appsv1.Deployment, *appsv1.Deployment, error) {
	raw, err := K8sClient(context.Background(), "get", "deployments", "-A", "-l", "app.kubernetes.io/part-of=olm", "-o", "jsonpath={.items}")
	if err != nil {
		return nil, nil, err
	}
	dl := []appsv1.Deployment{}
	if err := json.Unmarshal([]byte(raw), &dl); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal OLM deployments: %v", err)
	}

	var operatorController, catalogd *appsv1.Deployment
	for i := range dl {
		switch dl[i].Name {
		case olmDeploymentName:
			operatorController = &dl[i]
		case catalogdDeploymentName:
			catalogd = &dl[i]
		}
	}
	if operatorController == nil {
		return nil, nil, fmt.Errorf("failed to detect OLM Deployment")
	}
	return operatorController, catalogd, nil
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
	if out, err := K8sClient(context.Background(), "get", "nodes", "--no-headers", "-o", "name"); err == nil &&
		len(strings.Fields(strings.TrimSpace(out))) >= 2 {
		FeatureGates[catalogdHAFeature] = true
	}

	olm, catalogdDep, err := detectOLMDeployments()
	if err != nil {
		logger.Info("OLM deployments not found; skipping feature gate detection (upgrade scenarios will install OLM in Background)")
		return
	}
	OLMNamespace = olm.Namespace
	ComponentNamespaces["operator-controller"] = OLMNamespace

	// Catalogd may be in a different Namespace than operator-controller.
	catalogdNS := OLMNamespace
	if catalogdDep != nil {
		catalogdNS = catalogdDep.Namespace
	}
	ComponentNamespaces["catalogd"] = catalogdNS

	// Refine CatalogdHA based on actual catalogd replica count now that catalogdNS is
	// known.  The node-count check above can fire on any multi-node cluster even when
	// catalogd runs with only 1 replica.  Override the gate: HA scenarios require ≥2
	// catalogd replicas.  Fall back to whatever the node-count check set when catalogd
	// was not found or the replica count is not parseable.
	if catalogdDep != nil {
		if replicas := catalogdDep.Spec.Replicas; replicas != nil {
			FeatureGates[catalogdHAFeature] = *replicas >= 2
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
					FeatureGates[featuregate.Feature(matches[1])] = v
				}
			}
		}
	}

	logger.Info(fmt.Sprintf("Enabled feature gates: %v", FeatureGates))
}

func CheckFeatureTags(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	for _, tag := range sc.Tags {
		if enabled, found := FeatureGates[featuregate.Feature(tag.Name[1:])]; found && !enabled {
			logger.Info(fmt.Sprintf("Skipping scenario %q because feature gate %q is disabled", sc.Name, tag.Name[1:]))
			return ctx, godog.ErrSkip
		}
	}
	return ctx, nil
}

func CreateScenarioContext(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	// Godog IDs restart in each component process. Scope resource identities by
	// feature so concurrent suites cannot delete or modify each other's resources.
	featureName := strings.TrimSuffix(filepath.Base(sc.Uri), filepath.Ext(sc.Uri))
	featureHash := sha256.Sum256([]byte(featureName))
	id := fmt.Sprintf("%x-%s", featureHash[:4], sc.Id)
	scCtx := &World{
		ID:                   id,
		FeatureName:          featureName,
		ScenarioName:         sc.Name,
		Namespace:            fmt.Sprintf("ns-%s", id),
		ClusterExtensionName: fmt.Sprintf("ce-%s", id),
		ClusterObjectSetName: fmt.Sprintf("ce-%s-1", id),
		Catalogs:             make(map[string]string),
		CatalogPackageNames:  make(map[string]string),
		MetricsResponse:      make(map[string]string),
		LeaderPods:           make(map[string]string),
	}
	return context.WithValue(ctx, scenarioContextKey, scCtx), nil
}

func Scenario(ctx context.Context) *World {
	return ctx.Value(scenarioContextKey).(*World)
}

func stderrOutput(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr != nil {
		return string(exitErr.Stderr)
	}
	return ""
}

func ScenarioCleanup(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
	sc := Scenario(ctx)
	// Stop the in-process recording proxy if one was started.
	if sc.proxy != nil {
		sc.proxy.stop()
	}
	// Stop catalog port-forward if one was started.
	if sc.CatalogCleanup != nil {
		sc.CatalogCleanup()
	}

	// Restore any deployments that were modified during the scenario.  Runs
	// unconditionally (even on failure) to prevent a misconfigured deployment
	// from bleeding into subsequent scenarios.  Restored in LIFO order so that
	// multiple patches to the same deployment unwind to the true original.
	for i := len(sc.deploymentRestores) - 1; i >= 0; i-- {
		dr := sc.deploymentRestores[i]
		if dr.patchedArgs {
			if err2 := patchDeploymentArgs(dr.Namespace, dr.Name, dr.originalArgs); err2 != nil {
				logger.Info("Error restoring deployment args", "name", dr.Name, "error", err2)
			} else if _, err2 := K8sClient(ctx, "rollout", "status", "-n", dr.Namespace,
				fmt.Sprintf("deployment/%s", dr.Name), "--timeout=2m"); err2 != nil {
				logger.Info("Timeout waiting for deployment rollout after restore", "name", dr.Name)
			}
		}
		if err2 := restoreDeployment(dr); err2 != nil {
			logger.Info("Error restoring deployment env", "deployment", dr.Name, "namespace", dr.Namespace, "error", err2)
		}
	}

	if err != nil {
		return ctx, err
	}

	forDeletion := sc.AddedResources
	for _, catalogName := range sc.Catalogs {
		forDeletion = append(forDeletion, Resource{Name: catalogName, Kind: "clustercatalog"})
	}
	forDeletion = append(forDeletion, Resource{Name: sc.Namespace, Kind: "namespace"})

	g := new(errgroup.Group)
	g.SetLimit(8)
	for _, r := range forDeletion {
		g.Go(func() error {
			args := []string{"delete", r.Kind, r.Name, "--ignore-not-found=true"}
			if r.Namespace != "" {
				args = append(args, "-n", r.Namespace)
			}
			if _, err := K8sClient(ctx, args...); err != nil {
				logger.Info("Error deleting resource", "name", r.Name, "namespace", r.Namespace, "stderr", stderrOutput(err))
			}
			return nil
		})
	}
	_ = g.Wait()
	return ctx, nil
}

// Log returns the logger configured by BeforeSuite.
func Log() logr.Logger { return logger }

// CommandRecorder receives command output from reusable step helpers.
// Demo suites provide the implementation; other suites leave it unset.
type CommandRecorder interface {
	RecordCommand(command, stdout, stderr string, duration time.Duration)
	RecordCommandWithInput(command, stdin, stdout, stderr string, duration time.Duration)
	RecordCustom(command, stdout, stderr string)
}

type commandRecorderKey struct{}

// WithCommandRecorder attaches an optional command observer to a scenario.
func WithCommandRecorder(ctx context.Context, recorder CommandRecorder) context.Context {
	return context.WithValue(ctx, commandRecorderKey{}, recorder)
}

// CommandRecorderFromContext returns the scenario's command observer, if any.
func CommandRecorderFromContext(ctx context.Context) CommandRecorder {
	recorder, _ := ctx.Value(commandRecorderKey{}).(CommandRecorder)
	return recorder
}

// StderrOutput extracts captured stderr from a failed command.
func StderrOutput(err error) string { return stderrOutput(err) }
