package steps_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cucumber/godog"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	platformsteps "github.com/operator-framework/operator-controller/test/internal/e2e/platform/steps"
	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func TestResourceDisplayedWithColumns(t *testing.T) {
	for _, tt := range []struct {
		name     string
		resource string
		selector string
		want     string
	}{
		{
			name:     "named resource",
			resource: "configmap/settings-${SCENARIO_ID}",
			want:     "get configmap/settings-demo -n demo-namespace -o custom-columns=NAME:.metadata.name,MESSAGE:.data.message",
		},
		{
			name:     "revision history selected by group",
			resource: "clusterobjectsets",
			selector: "spec.group=cod-${SCENARIO_ID}",
			want:     "get clusterobjectsets -n demo-namespace -o custom-columns=NAME:.metadata.name,MESSAGE:.data.message --field-selector spec.group=cod-demo",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cli, err := exec.LookPath("echo")
			require.NoError(t, err)
			original := pflag.Lookup("k8s.cli").Value.String()
			require.NoError(t, pflag.Set("k8s.cli", cli))
			t.Cleanup(func() { require.NoError(t, pflag.Set("k8s.cli", original)) })

			ctx, err := shared.CreateScenarioContext(context.Background(), &godog.Scenario{Name: "Display resources"})
			require.NoError(t, err)
			shared.Scenario(ctx).ID = "demo"
			shared.Scenario(ctx).Namespace = "demo-namespace"
			dir := t.TempDir()
			recorder := platformsteps.NewAsciiCastRecorder("Display resources", dir)
			ctx = platformsteps.WithRecorder(ctx, recorder)
			recorder.BeginStep("Inspect resource ownership")

			err = platformsteps.ResourceDisplayedWithColumns(ctx, tt.resource, tt.selector, "NAME:.metadata.name,MESSAGE:.data.message")
			require.NoError(t, err)
			recorder.CommitStep()
			require.NoError(t, recorder.Stop())
			content, err := os.ReadFile(filepath.Join(dir, "display-resources.cast"))
			require.NoError(t, err)
			require.Contains(t, string(content), tt.want)
			require.Contains(t, string(content), "Inspect resource ownership")
			require.NotContains(t, string(content), "${")
		})
	}
}

func TestResourceDisplayedWithColumnsPropagatesCLIError(t *testing.T) {
	cli, err := exec.LookPath("false")
	require.NoError(t, err)
	original := pflag.Lookup("k8s.cli").Value.String()
	require.NoError(t, pflag.Set("k8s.cli", cli))
	t.Cleanup(func() { require.NoError(t, pflag.Set("k8s.cli", original)) })
	ctx, err := shared.CreateScenarioContext(context.Background(), &godog.Scenario{})
	require.NoError(t, err)

	err = platformsteps.ResourceDisplayedWithColumns(ctx, "configmap/missing", "", "NAME:.metadata.name")
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
}
