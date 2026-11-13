package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func readDeadlineObject(ctx context.Context, kind, name string, into any) error {
	name = shared.SubstituteScenarioVars(name, shared.Scenario(ctx))
	out, err := shared.K8sClient(ctx, "get", kind, name, "-o", "json")
	if err != nil {
		return fmt.Errorf("reading %s %q: %w", kind, name, err)
	}
	if err = json.Unmarshal([]byte(out), into); err != nil {
		return fmt.Errorf("decoding %s %q: %w", kind, name, err)
	}
	return nil
}

// ClusterObjectSetHasCompletion checks presence only. API validation separately
// guarantees that completedAt cannot be changed or removed once set.
func ClusterObjectSetHasCompletion(ctx context.Context, name string) error {
	var cos ocv1.ClusterObjectSet
	if err := readDeadlineObject(ctx, "clusterobjectset", name, &cos); err != nil {
		return err
	}
	if cos.Status.CompletedAt.IsZero() {
		return fmt.Errorf("revision %q has no completedAt", cos.Name)
	}
	return nil
}

// ClusterObjectSetCompletedBeforeDeadline compares durable timestamps on the
// named revision. The feature separately checks sole ownership.
func ClusterObjectSetCompletedBeforeDeadline(ctx context.Context, name, deployment string) error {
	var cos ocv1.ClusterObjectSet
	if err := readDeadlineObject(ctx, "clusterobjectset", name, &cos); err != nil {
		return err
	}
	var cod ocv1.ClusterObjectDeployment
	if err := readDeadlineObject(ctx, "clusterobjectdeployment", deployment, &cod); err != nil {
		return err
	}
	if cos.Spec.Group != cod.Name {
		return fmt.Errorf("revision %q is not in deployment group %q", cos.Name, cod.Name)
	}
	if cos.CreationTimestamp.IsZero() {
		return fmt.Errorf("revision %q has no creation timestamp", cos.Name)
	}
	if cos.Status.CompletedAt.IsZero() {
		return fmt.Errorf("revision %q has no completedAt", cos.Name)
	}
	if cos.Status.CompletedAt.Before(&cos.CreationTimestamp) {
		return fmt.Errorf("completedAt precedes revision creation")
	}
	if cod.Spec.ProgressDeadlineMinutes <= 0 {
		return fmt.Errorf("deployment %q has no enabled deadline", cod.Name)
	}
	deadline := cos.CreationTimestamp.Add(time.Duration(cod.Spec.ProgressDeadlineMinutes) * time.Minute)
	if !cos.Status.CompletedAt.Time.Before(deadline) {
		return fmt.Errorf("revision completedAt %s must be before deadline %s", cos.Status.CompletedAt, deadline)
	}
	return nil
}
