package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

// ClusterObjectSetKeepsLaterPhaseResourcesAbsent observes a blocked COS and all
// listed resources throughout a window. An API error cannot establish absence.
func ClusterObjectSetKeepsLaterPhaseResourcesAbsent(ctx context.Context, name, durationStr string, table *godog.Table) error {
	duration, err := time.ParseDuration(durationStr)
	if err != nil || duration <= 0 {
		return fmt.Errorf("expected a positive observation duration, got %q", durationStr)
	}
	if table == nil || len(table.Rows) < 2 || len(table.Rows[0].Cells) != 1 || table.Rows[0].Cells[0].Value != "resource" {
		return fmt.Errorf("expected a resource table with a 'resource' header and at least one row")
	}

	sc := shared.Scenario(ctx)
	name = shared.SubstituteScenarioVars(name, sc)
	type resourceRef struct{ kind, name string }
	resources := make([]resourceRef, 0, len(table.Rows)-1)
	for _, row := range table.Rows[1:] {
		if len(row.Cells) != 1 {
			return fmt.Errorf("expected one resource per row")
		}
		resource := shared.SubstituteScenarioVars(row.Cells[0].Value, sc)
		kind, resourceName, found := strings.Cut(resource, "/")
		if !found || kind == "" || resourceName == "" || strings.Contains(resourceName, "/") {
			return fmt.Errorf("resource %q is not in the format <kind>/<name>", resource)
		}
		resources = append(resources, resourceRef{kind, resourceName})
	}

	var start time.Time
	return wait.PollUntilContextTimeout(ctx, shared.Tick, duration+shared.Timeout, true, func(ctx context.Context) (bool, error) {
		resource, err := shared.ReadResourceConditions(ctx, "clusterobjectset", name)
		if err != nil {
			return false, err
		}
		ready := shared.CurrentCondition(resource.Metadata.Generation, resource.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
		if !shared.ConditionMatches(ready, metav1.ConditionFalse, ptr.To(ocv1.ClusterObjectSetReasonObjectsNotReady), nil) {
			return false, fmt.Errorf("ClusterObjectSet %q no longer reports current-generation Ready=False/ObjectsNotReady: %v", name, ready)
		}
		for _, ref := range resources {
			out, err := shared.K8sClient(ctx, "get", ref.kind, ref.name, "-n", sc.Namespace, "--ignore-not-found", "-o", "name")
			if err != nil {
				return false, fmt.Errorf("checking absence of %s/%s: %w", ref.kind, ref.name, err)
			}
			if strings.TrimSpace(out) != "" {
				return false, fmt.Errorf("later phase resource %s/%s appeared while ClusterObjectSet %q was blocked", ref.kind, ref.name, name)
			}
		}
		if start.IsZero() {
			start = time.Now()
		}
		return time.Since(start) >= duration, nil
	})
}
