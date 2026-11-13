package steps

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

// Only the API transport is replaced. Completion checks use the real helpers;
// ordinary count/condition steps retain their existing tests.
func deadlineFixture(t *testing.T) (context.Context, *ocv1.ClusterObjectDeployment, *ocv1.ClusterObjectSet, string) {
	t.Helper()
	ctx, err := shared.CreateScenarioContext(t.Context(), &godog.Scenario{Id: "1", Uri: "cod_progress_deadline.feature"})
	require.NoError(t, err)
	sc := shared.Scenario(ctx)
	cod := &ocv1.ClusterObjectDeployment{ObjectMeta: metav1.ObjectMeta{Name: sc.ClusterExtensionName}}
	cod.Spec.ProgressDeadlineMinutes = 1
	cos := &ocv1.ClusterObjectSet{ObjectMeta: metav1.ObjectMeta{Name: cod.Name + "-1", CreationTimestamp: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}}
	cos.Spec.Group = cod.Name
	cos.Status.CompletedAt = metav1.NewTime(cos.CreationTimestamp.Add(10 * time.Second))
	dir := t.TempDir()
	t.Setenv("DEADLINE_FIXTURES", dir)
	script := `#!/bin/sh
if [ -f "$DEADLINE_FIXTURES/api-error" ]; then echo "API unavailable" >&2; exit 1; fi
case "$1 $2" in
 "get clusterobjectdeployment") cat "$DEADLINE_FIXTURES/cod.json";;
 "get clusterobjectset") cat "$DEADLINE_FIXTURES/cos.json";;
 *) echo "unexpected command: $*" >&2; exit 1;;
esac
`
	cli := filepath.Join(dir, "kubectl")
	//nolint:gosec // Private executable fixture.
	require.NoError(t, os.WriteFile(cli, []byte(script), 0o700))
	oldCLI := pflag.Lookup("k8s.cli").Value.String()
	require.NoError(t, pflag.Set("k8s.cli", cli))
	t.Cleanup(func() { require.NoError(t, pflag.Set("k8s.cli", oldCLI)) })
	writeDeadlineFixture(t, dir, cod, cos)
	return ctx, cod, cos, dir
}

func writeDeadlineJSON(t *testing.T, dir, name string, obj any) {
	t.Helper()
	data, err := json.Marshal(obj)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
}
func writeDeadlineFixture(t *testing.T, dir string, cod *ocv1.ClusterObjectDeployment, cos *ocv1.ClusterObjectSet) {
	t.Helper()
	writeDeadlineJSON(t, dir, "cod.json", cod)
	writeDeadlineJSON(t, dir, "cos.json", cos)
}

func TestDeadlineTimestampComparison(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ocv1.ClusterObjectDeployment, *ocv1.ClusterObjectSet)
		want   string
	}{
		{name: "before expiry"},
		{
			name: "after one minute but before configured deadline",
			mutate: func(cod *ocv1.ClusterObjectDeployment, cos *ocv1.ClusterObjectSet) {
				cod.Spec.ProgressDeadlineMinutes = 3
				cos.Status.CompletedAt = metav1.NewTime(cos.CreationTimestamp.Add(96 * time.Second))
			},
		},
		{name: "exact expiry", mutate: func(_ *ocv1.ClusterObjectDeployment, c *ocv1.ClusterObjectSet) {
			c.Status.CompletedAt = metav1.NewTime(c.CreationTimestamp.Add(time.Minute))
		}, want: "before"},
		{name: "after expiry", mutate: func(_ *ocv1.ClusterObjectDeployment, c *ocv1.ClusterObjectSet) {
			c.Status.CompletedAt = metav1.NewTime(c.CreationTimestamp.Add(61 * time.Second))
		}, want: "before"},
		{name: "before creation", mutate: func(_ *ocv1.ClusterObjectDeployment, c *ocv1.ClusterObjectSet) {
			c.Status.CompletedAt = metav1.NewTime(c.CreationTimestamp.Add(-time.Second))
		}, want: "creation"},
		{name: "missing creation", mutate: func(_ *ocv1.ClusterObjectDeployment, c *ocv1.ClusterObjectSet) { c.CreationTimestamp = metav1.Time{} }, want: "creation"},
		{name: "missing completion", mutate: func(_ *ocv1.ClusterObjectDeployment, c *ocv1.ClusterObjectSet) { c.Status.CompletedAt = metav1.Time{} }, want: "completedAt"},
		{name: "disabled deadline", mutate: func(c *ocv1.ClusterObjectDeployment, _ *ocv1.ClusterObjectSet) { c.Spec.ProgressDeadlineMinutes = 0 }, want: "deadline"},
		{name: "different group", mutate: func(_ *ocv1.ClusterObjectDeployment, c *ocv1.ClusterObjectSet) { c.Spec.Group = "another" }, want: "group"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cod, cos, dir := deadlineFixture(t)
			if tc.mutate != nil {
				tc.mutate(cod, cos)
			}
			writeDeadlineFixture(t, dir, cod, cos)
			err := ClusterObjectSetCompletedBeforeDeadline(ctx, "${NAME}-1", "${NAME}")
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
}

func TestClusterObjectSetHasCompletion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		apiError bool
		wantErr  string
	}{
		{
			name:     "present",
			response: `{"status":{"completedAt":"2026-01-01T00:00:10Z"}}`,
		},
		{
			name:     "missing",
			response: `{"status":{}}`,
			wantErr:  "has no completedAt",
		},
		{
			name:     "API error",
			apiError: true,
			wantErr:  "reading clusterobjectset",
		},
		{
			name:     "invalid JSON",
			response: "invalid",
			wantErr:  "decoding clusterobjectset",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, _, dir := deadlineFixture(t)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "cos.json"), []byte(tc.response), 0o600))
			if tc.apiError {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "api-error"), nil, 0o600))
			}
			err := ClusterObjectSetHasCompletion(ctx, "${NAME}-1")
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}
