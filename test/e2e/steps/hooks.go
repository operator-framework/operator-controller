package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

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
	name string
	kind string
}

type scenarioContext struct {
	id                           string
	namespace                    string
	clusterExtensionName         string
	clusterExtensionRevisionName string
	clusterCatalogName           string
	addedResources               []resource
	removedResources             []unstructured.Unstructured
	backGroundCmds               []*exec.Cmd
	metricsResponse              map[string]string
	leaderPods                   map[string]string // component name -> leader pod name

	extensionObjects []client.Object
}

// GatherClusterExtensionObjects collects all resources related to the ClusterExtension container in
// either their Helm release Secret or ClusterExtensionRevision depending on the applier being used
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
		id:                           sc.Id,
		namespace:                    fmt.Sprintf("ns-%s", sc.Id),
		clusterExtensionName:         fmt.Sprintf("ce-%s", sc.Id),
		clusterExtensionRevisionName: fmt.Sprintf("cer-%s", sc.Id),
		leaderPods:                   make(map[string]string),
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
	for _, bgCmd := range sc.backGroundCmds {
		if p := bgCmd.Process; p != nil {
			_ = p.Kill()
		}
	}
	if err != nil {
		return ctx, err
	}

	forDeletion := sc.addedResources
	if sc.clusterExtensionName != "" {
		forDeletion = append(forDeletion, resource{name: sc.clusterExtensionName, kind: "clusterextension"})
	}
	if sc.clusterExtensionRevisionName != "" {
		forDeletion = append(forDeletion, resource{name: sc.clusterExtensionRevisionName, kind: "clusterextensionrevision"})
	}
	forDeletion = append(forDeletion, resource{name: sc.namespace, kind: "namespace"})
	for _, r := range forDeletion {
		go func(res resource) {
			if _, err := k8sClient("delete", res.kind, res.name, "--ignore-not-found=true"); err != nil {
				logger.Info("Error deleting resource", "name", res.name, "namespace", sc.namespace, "stderr", stderrOutput(err))
			}
		}(r)
	}
	return ctx, nil
}
