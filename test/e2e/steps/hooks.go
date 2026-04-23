package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"sync"

	"github.com/cucumber/godog"
	"github.com/go-logr/logr"
	"github.com/spf13/pflag"
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

// deploymentRestore records the original container args of a deployment so that
// it can be patched back to its pre-test state during scenario cleanup.
type deploymentRestore struct {
	namespace      string
	deploymentName string
	originalArgs   []string
}

type scenarioContext struct {
	id                   string
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

	extensionObjects []client.Object
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
	devMode      = false
	featureGates = map[featuregate.Feature]bool{
		features.WebhookProviderCertManager:        true,
		features.PreflightPermissions:              false,
		features.SingleOwnNamespaceInstallSupport:  false,
		features.SyntheticPermissions:              false,
		features.WebhookProviderOpenshiftServiceCA: false,
		features.HelmChartSupport:                  false,
		features.BoxcutterRuntime:                  false,
		features.DeploymentConfig:                  false,
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

	sc.After(ScenarioCleanup)
}

func detectOLMDeployment() (*appsv1.Deployment, error) {
	raw, err := k8sClient("get", "deployments", "-A", "-l", "app.kubernetes.io/part-of=olm", "-o", "jsonpath={.items}")
	if err != nil {
		return nil, err
	}
	dl := []appsv1.Deployment{}
	if err := json.Unmarshal([]byte(raw), &dl); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OLM deployments: %v", err)
	}

	for _, d := range dl {
		if d.Name == olmDeploymentName {
			return &d, nil
		}
	}
	return nil, fmt.Errorf("failed to detect OLM Deployment")
}

func BeforeSuite() {
	if devMode {
		logger = textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(1)))
	} else {
		logger = textlogger.NewLogger(textlogger.NewConfig())
	}

	olm, err := detectOLMDeployment()
	if err != nil {
		logger.Info("OLM deployments not found; skipping feature gate detection (upgrade scenarios will install OLM in Background)")
		return
	}
	olmNamespace = olm.Namespace

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
			logger.Info(fmt.Sprintf("Skipping scenario %q because feature gate %q is disabled", sc.Name, tag.Name[1:]))
			return ctx, godog.ErrSkip
		}
	}
	return ctx, nil
}

func CreateScenarioContext(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	scCtx := &scenarioContext{
		id:                   sc.Id,
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
	// Always restore deployments whose args were modified during the scenario,
	// even when the scenario failed, so that a misconfigured TLS profile does
	// not leak into subsequent scenarios.  Restore in reverse order so that
	// multiple patches to the same deployment unwind back to the true original.
	for i := len(sc.deploymentRestores) - 1; i >= 0; i-- {
		dr := sc.deploymentRestores[i]
		if err2 := patchDeploymentArgs(dr.namespace, dr.deploymentName, dr.originalArgs); err2 != nil {
			logger.Info("Error restoring deployment args", "name", dr.deploymentName, "error", err2)
			continue
		}
		if _, err2 := k8sClient("rollout", "status", "-n", dr.namespace,
			fmt.Sprintf("deployment/%s", dr.deploymentName), "--timeout=2m"); err2 != nil {
			logger.Info("Timeout waiting for deployment rollout after restore", "name", dr.deploymentName)
		}
	}

	if err != nil {
		return ctx, err
	}

	forDeletion := sc.addedResources
	if sc.clusterExtensionName != "" {
		forDeletion = append(forDeletion, resource{name: sc.clusterExtensionName, kind: "clusterextension"})
	}
	if sc.clusterObjectSetName != "" && featureGates[features.BoxcutterRuntime] {
		forDeletion = append(forDeletion, resource{name: sc.clusterObjectSetName, kind: "clusterobjectset"})
	}
	for _, catalogName := range sc.catalogs {
		forDeletion = append(forDeletion, resource{name: catalogName, kind: "clustercatalog"})
	}
	forDeletion = append(forDeletion, resource{name: sc.namespace, kind: "namespace"})

	var wg sync.WaitGroup
	for _, r := range forDeletion {
		wg.Add(1)
		go func(res resource) {
			defer wg.Done()
			args := []string{"delete", res.kind, res.name, "--ignore-not-found=true"}
			if res.namespace != "" {
				args = append(args, "-n", res.namespace)
			}
			if _, err := k8sClient(args...); err != nil {
				logger.Info("Error deleting resource", "name", res.name, "namespace", res.namespace, "stderr", stderrOutput(err))
			}
		}(r)
	}
	wg.Wait()
	return ctx, nil
}
