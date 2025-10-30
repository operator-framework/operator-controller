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
	"k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type resource struct {
	name string
	kind string
}

type scenarioContext struct {
	id                   string
	namespace            string
	clusterExtensionName string
	addedResources       []resource
	removedResources     []unstructured.Unstructured
	backGroundCmds       []*exec.Cmd
	metricsResponse      string
}

type contextKey string

const (
	scenarioContextKey contextKey = "scenario-context"
)

var featureGates = map[string]bool{
	"WebhookProviderCertManager": true,
}

func RegisterHooks(sc *godog.ScenarioContext) {
	sc.Before(CheckFeatureTags)
	sc.Before(CreateScenarioContext)

	sc.After(ScenarioCleanup)
}

func DetectEnabledFeatureGates() {
	raw, err := kubectl("get", "deployment", "-n", olmNamespace, olmDeploymentName, "-o", "json")
	if err != nil {
		return
	}
	d := &v1.Deployment{}
	if err := json.Unmarshal([]byte(raw), d); err != nil {
		return
	}

	featureGatePattern := regexp.MustCompile(`--feature-gates=([[:alnum:]]+)=(true|false)`)
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name == "manager" {
			for _, arg := range c.Args {
				if matches := featureGatePattern.FindStringSubmatch(arg); matches != nil {
					v, _ := strconv.ParseBool(matches[2])
					featureGates[matches[1]] = v
				}
			}
		}
	}
	logger.Info(fmt.Sprintf("Enabled feature gates: %v", featureGates))
}

func CheckFeatureTags(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	for _, tag := range sc.Tags {
		if enabled, found := featureGates[tag.Name[1:]]; !found || (found && !enabled) {
			logger.V(1).Info(fmt.Sprintf("Skipping scenario %q because feature gate %q is disabled", sc.Name, tag.Name[1:]))
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
	}
	return context.WithValue(ctx, scenarioContextKey, scCtx), nil
}

func scenarioCtx(ctx context.Context) *scenarioContext {
	return ctx.Value(scenarioContextKey).(*scenarioContext)
}

func ScenarioCleanup(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
	sc := scenarioCtx(ctx)
	for _, p := range sc.backGroundCmds {
		p.Process.Kill() // nolint: errcheck // we don't care about the error here, we just want to kill the process
		p.Process.Wait() // nolint: errcheck // same as above, we just want to wait for the process to exit, and do not want to fail the test if it does not
	}
	if err != nil {
		return ctx, err
	}

	forDeletion := []resource{}
	if sc.clusterExtensionName != "" {
		forDeletion = append(forDeletion, resource{name: sc.clusterExtensionName, kind: "clusterextension"})
	}
	forDeletion = append(forDeletion, sc.addedResources...)
	forDeletion = append(forDeletion, resource{name: sc.namespace, kind: "namespace"})
	for _, r := range forDeletion {
		if _, err := kubectl("delete", r.kind, r.name, "-n", sc.namespace); err != nil {
			logger.Info("Error deleting resource", "name", r.name, "namespace", sc.namespace, "stderr", string(func() *exec.ExitError {
				target := &exec.ExitError{}
				_ = errors.As(err, &target)
				return target
			}().Stderr))
		}
	}
	return ctx, nil
}
