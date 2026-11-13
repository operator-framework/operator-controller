package steps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func TestLaterPhaseResourcesStayAbsent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       string
		generation int64
		status     metav1.ConditionStatus
		reason     string
		duration   string
		wantError  string
	}{
		{name: "all resources absent", generation: 2, status: metav1.ConditionFalse, reason: ocv1.ClusterObjectSetReasonObjectsNotReady},
		{name: "resource already present", mode: "present", generation: 2, status: metav1.ConditionFalse, reason: ocv1.ClusterObjectSetReasonObjectsNotReady, wantError: "later phase resource configmap/second appeared"},
		{name: "resource appears later", mode: "late", generation: 2, status: metav1.ConditionFalse, reason: ocv1.ClusterObjectSetReasonObjectsNotReady, duration: "1s", wantError: "later phase resource configmap/third appeared"},
		{name: "stale condition", generation: 1, status: metav1.ConditionFalse, reason: ocv1.ClusterObjectSetReasonObjectsNotReady, wantError: "current-generation Ready=False/ObjectsNotReady"},
		{name: "probe no longer blocks", generation: 2, status: metav1.ConditionTrue, reason: ocv1.ClusterObjectSetReasonAllObjectsReady, wantError: "current-generation Ready=False/ObjectsNotReady"},
		{name: "different failure", generation: 2, status: metav1.ConditionFalse, reason: ocv1.ClusterObjectSetReasonObjectCollision, wantError: "current-generation Ready=False/ObjectsNotReady"},
		{name: "condition read fails", mode: "condition-error", wantError: "reading clusterobjectset"},
		{name: "condition response is invalid", mode: "invalid-json", wantError: "unmarshaling clusterobjectset"},
		{name: "absence read fails", mode: "resource-error", generation: 2, status: metav1.ConditionFalse, reason: ocv1.ClusterObjectSetReasonObjectsNotReady, wantError: "checking absence of configmap/second"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("E2E_LIFECYCLE_FIXTURES", dir)
			t.Setenv("E2E_LIFECYCLE_MODE", tc.mode)
			cli := filepath.Join(dir, "kubectl")
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$E2E_LIFECYCLE_FIXTURES/commands"
case "$1 $2" in
  "get clusterobjectset")
    case "$E2E_LIFECYCLE_MODE" in
      condition-error) echo "API unavailable" >&2; exit 1;;
      invalid-json) echo "invalid JSON"; exit 0;;
    esac
    count=0
    if [ -f "$E2E_LIFECYCLE_FIXTURES/reads" ]; then
      count=$(cat "$E2E_LIFECYCLE_FIXTURES/reads")
    fi
    echo "$((count + 1))" > "$E2E_LIFECYCLE_FIXTURES/reads"
    cat "$E2E_LIFECYCLE_FIXTURES/cos.json"
    ;;
  "get configmap")
    case "$E2E_LIFECYCLE_MODE" in
      present) printf 'configmap/%s\n' "$3";;
      late)
        if [ "$3" = third ] && [ "$(cat "$E2E_LIFECYCLE_FIXTURES/reads")" -gt 1 ]; then
          echo "configmap/third"
        fi
        ;;
      resource-error) echo "API unavailable" >&2; exit 1;;
    esac
    ;;
  *) echo "unexpected command: $*" >&2; exit 1;;
esac
`
			//nolint:gosec // The private test CLI must be executable.
			require.NoError(t, os.WriteFile(cli, []byte(script), 0o700))
			oldCLI := pflag.Lookup("k8s.cli").Value.String()
			require.NoError(t, pflag.Set("k8s.cli", cli))
			t.Cleanup(func() { require.NoError(t, pflag.Set("k8s.cli", oldCLI)) })

			cos := ocv1.ClusterObjectSet{ObjectMeta: metav1.ObjectMeta{Generation: 2}}
			cos.Status.Conditions = []metav1.Condition{{
				Type: ocv1.ClusterObjectSetTypeReady, ObservedGeneration: tc.generation,
				Status: tc.status, Reason: tc.reason,
			}}
			data, err := json.Marshal(cos)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "cos.json"), data, 0o600))
			ctx, err := shared.CreateScenarioContext(t.Context(), &godog.Scenario{Id: "1", Uri: "cos_object_lifecycle.feature"})
			require.NoError(t, err)
			table := new(godog.Table)
			require.NoError(t, json.Unmarshal([]byte(`{"rows":[
				{"cells":[{"value":"resource"}]},
				{"cells":[{"value":"configmap/second"}]},
				{"cells":[{"value":"configmap/third"}]}
			]}`), table))
			duration := tc.duration
			if duration == "" {
				duration = "1ns"
			}
			err = ClusterObjectSetKeepsLaterPhaseResourcesAbsent(ctx, "${COS_NAME}", duration, table)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			commands, err := os.ReadFile(filepath.Join(dir, "commands"))
			require.NoError(t, err)
			sc := shared.Scenario(ctx)
			require.Contains(t, string(commands), "get clusterobjectset "+sc.ClusterObjectSetName+" -o json")
			for _, name := range []string{"second", "third"} {
				require.Contains(t, string(commands), "get configmap "+name+" -n "+sc.Namespace+" --ignore-not-found -o name")
			}
		})
	}
}

func TestLaterPhaseObservationRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		duration  string
		rows      string
		wantError string
	}{
		{name: "invalid duration", duration: "invalid", wantError: "positive observation duration"},
		{name: "zero duration", duration: "0s", wantError: "positive observation duration"},
		{name: "negative duration", duration: "-1s", wantError: "positive observation duration"},
		{name: "missing table", duration: "1s", wantError: "expected a resource table"},
		{name: "empty table", duration: "1s", rows: `[]`, wantError: "expected a resource table"},
		{name: "header only", duration: "1s", rows: `[{"cells":[{"value":"resource"}]}]`, wantError: "expected a resource table"},
		{name: "wrong header", duration: "1s", rows: `[{"cells":[{"value":"name"}]},{"cells":[{"value":"configmap/late"}]}]`, wantError: "expected a resource table"},
		{name: "extra cells", duration: "1s", rows: `[{"cells":[{"value":"resource"}]},{"cells":[{"value":"configmap/late"},{"value":"extra"}]}]`, wantError: "one resource per row"},
		{name: "missing separator", duration: "1s", rows: `[{"cells":[{"value":"resource"}]},{"cells":[{"value":"late"}]}]`, wantError: "<kind>/<name>"},
		{name: "empty kind", duration: "1s", rows: `[{"cells":[{"value":"resource"}]},{"cells":[{"value":"/late"}]}]`, wantError: "<kind>/<name>"},
		{name: "empty name", duration: "1s", rows: `[{"cells":[{"value":"resource"}]},{"cells":[{"value":"configmap/"}]}]`, wantError: "<kind>/<name>"},
		{name: "extra separator", duration: "1s", rows: `[{"cells":[{"value":"resource"}]},{"cells":[{"value":"configmap/late/extra"}]}]`, wantError: "<kind>/<name>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, err := shared.CreateScenarioContext(t.Context(), &godog.Scenario{Id: "1", Uri: "cos_object_lifecycle.feature"})
			require.NoError(t, err)
			var table *godog.Table
			if tc.rows != "" {
				require.NoError(t, json.Unmarshal([]byte(`{"rows":`+strings.TrimSpace(tc.rows)+`}`), &table))
			}
			err = ClusterObjectSetKeepsLaterPhaseResourcesAbsent(ctx, "cos", tc.duration, table)
			require.ErrorContains(t, err, tc.wantError)
		})
	}
}
