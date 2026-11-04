package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cucumber/godog"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func TestScenarioResourceIsolationAcrossSuites(t *testing.T) {
	// Exercise substitution through the real apply step using a CLI that records
	// its input. No cluster is needed to reproduce Godog IDs restarting at 1.
	dir := t.TempDir()
	cli := filepath.Join(dir, "kubectl")
	require.NoError(t, os.WriteFile(cli, []byte("#!/bin/sh\ncat > \"$E2E_APPLIED_RESOURCE\"\n"), 0o700))
	original := pflag.Lookup("k8s.cli").Value.String()
	require.NoError(t, pflag.Set("k8s.cli", cli))
	t.Cleanup(func() { require.NoError(t, pflag.Set("k8s.cli", original)) })
	output := filepath.Join(dir, "applied.yaml")
	t.Setenv("E2E_APPLIED_RESOURCE", output)
	var names []string
	for _, uri := range []string{"features/install.feature", "features/revision.feature", "object-controller/features/revision.feature"} {
		ctx, err := steps.CreateScenarioContext(context.Background(), &godog.Scenario{Id: "1", Name: "Stable scenario name", Uri: uri})
		require.NoError(t, err)
		require.NoError(t, steps.ResourceIsApplied(ctx, &godog.DocString{Content: `apiVersion: v1
kind: Namespace
metadata:
  name: ${TEST_NAMESPACE}
  labels:
    scenario-id: ${SCENARIO_ID}
`}))
		data, err := os.ReadFile(output)
		require.NoError(t, err)
		var obj unstructured.Unstructured
		require.NoError(t, yaml.Unmarshal(data, &obj.Object))
		names = append(names, obj.GetName())
		require.Equal(t, "Stable scenario name", obj.GetAnnotations()["e2e.olm.operatorframework.io/scenario"])
		require.Equal(t, "ns-"+obj.GetLabels()["scenario-id"], obj.GetName())
	}
	require.NotEqual(t, names[0], names[1], "different features with the same Godog ID need distinct resources")
	require.Equal(t, names[1], names[2], "moving a feature must preserve its resource identity")
}
