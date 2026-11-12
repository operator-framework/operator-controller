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
	"k8s.io/apimachinery/pkg/util/sets"
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

// World holds scenario state shared by component step packages.
type World struct {
	ID                   string
	FeatureName          string
	Tags                 sets.Set[string]
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
	ExtensionObjects     []client.Object
	RevisionSecrets      []Resource
	ResourceUIDs         map[string]types.UID
	CatalogAddr          string
	CatalogCleanup       func()
	LastApplyStderr      string
}

type contextKey string

const (
	scenarioContextKey contextKey = "scenario-context"
)

var (
	devMode              = false
	objectControllerOnly bool
	// FeatureGates holds defaults refined by BeforeSuite from the cluster configuration.
	// Consumers must not mutate this map.
	FeatureGates = map[featuregate.Feature]bool{
		features.WebhookProviderCertManager:        true,
		features.SingleOwnNamespaceInstallSupport:  false,
		features.WebhookProviderOpenshiftServiceCA: false,
		features.BoxcutterRuntime:                  false,
		features.DeploymentConfig:                  false,
		featuregate.Feature("CatalogdHA"):          false,
		ObjectControllerFeature:                    false,
	}
	logger logr.Logger
)

func init() {
	flagSet := pflag.CommandLine
	flagSet.BoolVar(&devMode, "log.debug", false, "print debug log level")
	flagSet.BoolVar(&objectControllerOnly, "e2e.object-controller-only", false, "require object-controller without ClusterExtension or ClusterCatalog APIs/controllers")
}

// ObjectControllerOnly reports whether the run must validate standalone isolation.
func ObjectControllerOnly() bool { return objectControllerOnly }

// RegisterHooks installs shared lifecycle hooks, with optional suite hooks between setup and cleanup.
func RegisterHooks(sc *godog.ScenarioContext, hooks ...func(*godog.ScenarioContext)) {
	sc.Before(CheckFeatureTags)
	sc.Before(CreateScenarioContext)
	for _, register := range hooks {
		register(sc)
	}

	sc.After(ScenarioCleanup)
}

// detectOLMDeployments discovers components independently across namespaces.
// Missing components are valid for standalone installations and upgrade scenarios.
func detectOLMDeployments() (map[string]*appsv1.Deployment, error) {
	raw, err := K8sClient(context.Background(), "get", "deployments", "-A", "-l", "app.kubernetes.io/part-of=olm", "-o", "jsonpath={.items}")
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
		case OperatorControllerDeploymentName:
			deployments[OperatorControllerComponent] = &dl[i]
		case CatalogdDeploymentName:
			deployments[CatalogdComponent] = &dl[i]
		case ObjectControllerDeploymentName:
			deployments[ObjectControllerComponent] = &dl[i]
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
	if out, err := K8sClient(context.Background(), "get", "nodes", "--no-headers", "-o", "name"); err == nil &&
		len(strings.Fields(strings.TrimSpace(out))) >= 2 {
		FeatureGates[featuregate.Feature("CatalogdHA")] = true
	}

	deployments, err := detectOLMDeployments()
	if err != nil {
		logger.Info("OLM deployments not found; skipping feature gate detection (upgrade scenarios will install OLM in Background)")
		return
	}
	configureControllerFeatures(deployments)
}

// configureControllerFeatures refreshes namespaces and feature gates from the
// independently discovered controller deployments.
func configureControllerFeatures(deployments map[string]*appsv1.Deployment) {
	// Clear namespaces from any previous discovery, including removed components.
	clear(ComponentNamespaces)
	for component, deployment := range deployments {
		ComponentNamespaces[component] = deployment.Namespace
	}
	object := deployments[ObjectControllerComponent]
	FeatureGates[ObjectControllerFeature] = object != nil
	olm, catalogdDep := deployments[OperatorControllerComponent], deployments[CatalogdComponent]
	if olm != nil {
		OLMNamespace = olm.Namespace
	} else if object != nil {
		// Shared Secret helpers default omitted namespaces to the system namespace.
		OLMNamespace = object.Namespace
	}

	// Catalogd may be in a different namespace than operator-controller.
	if catalogdDep == nil && olm != nil {
		ComponentNamespaces[CatalogdComponent] = OLMNamespace
	}

	// Refine CatalogdHA based on actual catalogd replica count now that catalogdNS is
	// known.  The node-count check above can fire on any multi-node cluster even when
	// catalogd runs with only 1 replica.  Override the gate: HA scenarios require ≥2
	// catalogd replicas.  Fall back to whatever the node-count check set when catalogd
	// was not found or the replica count is not parseable.
	if catalogdDep != nil {
		if replicas := catalogdDep.Spec.Replicas; replicas != nil {
			FeatureGates[featuregate.Feature("CatalogdHA")] = *replicas >= 2
		}
	}

	if olm == nil {
		logger.Info("operator-controller is absent; using independently detected components")
		return
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
			// Explicit standalone and integrated Boxcutter runs must reach
			// readiness so a missing required controller fails instead of skipping.
			if tag.Name == "@"+string(ObjectControllerFeature) && (objectControllerOnly || FeatureGates[features.BoxcutterRuntime]) {
				continue
			}
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
		Tags:                 sets.New[string](),
		ScenarioName:         sc.Name,
		Namespace:            fmt.Sprintf("ns-%s", id),
		ClusterExtensionName: fmt.Sprintf("ce-%s", id),
		ClusterObjectSetName: fmt.Sprintf("ce-%s-1", id),
		Catalogs:             make(map[string]string),
		CatalogPackageNames:  make(map[string]string),
		MetricsResponse:      make(map[string]string),
		LeaderPods:           make(map[string]string),
	}
	for _, tag := range sc.Tags {
		scCtx.Tags.Insert(tag.Name)
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
	// Stop catalog port-forward if one was started.
	if sc.CatalogCleanup != nil {
		sc.CatalogCleanup()
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
