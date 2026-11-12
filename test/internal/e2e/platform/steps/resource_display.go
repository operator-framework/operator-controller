package steps

import (
	"context"

	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

// ResourceDisplayedWithColumns records a concise resource view after the scenario's
// assertions have established readiness. An optional field selector scopes lists.
func ResourceDisplayedWithColumns(ctx context.Context, resource, fieldSelector, columns string) error {
	sc := shared.Scenario(ctx)
	args := []string{
		"get", shared.SubstituteScenarioVars(resource, sc),
		"-n", sc.Namespace,
		"-o", "custom-columns=" + shared.SubstituteScenarioVars(columns, sc),
	}
	if fieldSelector != "" {
		args = append(args, "--field-selector", shared.SubstituteScenarioVars(fieldSelector, sc))
	}
	_, err := shared.K8sClient(ctx, args...)
	return err
}
