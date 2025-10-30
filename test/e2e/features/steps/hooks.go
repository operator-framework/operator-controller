package steps

import (
	"context"
	"encoding/json"
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

const (
	scenarioContextKey = "scenario-context"
)

var featureGates = map[string]bool{
	"WebhookProviderCertManager": true,
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
	fmt.Println("Enabled feature gates:", featureGates)
}

func CheckFeatureTags(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	for _, tag := range sc.Tags {
		if enabled, found := featureGates[tag.Name[1:]]; !found || (found && !enabled) {
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
		p.Process.Kill()
		p.Process.Wait()
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
			fmt.Println("Error deleting resource:", r.name, err)
			//return ctx, err
		}
	}
	return ctx, nil
}
