package steps_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cucumber/godog"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	platformsteps "github.com/operator-framework/operator-controller/test/internal/e2e/platform/steps"
	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func TestResourceControlledByClusterObjectSet(t *testing.T) {
	for _, tt := range []struct {
		name       string
		ownerUID   string
		controller bool
		wantError  bool
	}{
		{name: "current revision controls resource", ownerUID: "revision-uid", controller: true},
		{name: "same name with stale UID", ownerUID: "stale-uid", controller: true, wantError: true},
		{name: "non-controller owner", ownerUID: "revision-uid", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cli := filepath.Join(t.TempDir(), "kubectl")
			script := fmt.Sprintf(`#!/bin/sh
case "$2/$3" in
  configmap/settings-demo)
    echo '{"apiVersion":"v1","kind":"ConfigMap","metadata":{"ownerReferences":[{"apiVersion":"olm.operatorframework.io/v1","kind":"ClusterObjectSet","name":"cod-demo-1","uid":"%s","controller":%t}]}}'
    ;;
  clusterobjectset/cod-demo-1)
    echo '{"apiVersion":"olm.operatorframework.io/v1","kind":"ClusterObjectSet","metadata":{"name":"cod-demo-1","uid":"revision-uid"}}'
    ;;
  *) exit 1 ;;
esac
`, tt.ownerUID, tt.controller)
			require.NoError(t, os.WriteFile(cli, []byte(script), 0o700)) //nolint:gosec // The test CLI must be executable.
			original := pflag.Lookup("k8s.cli").Value.String()
			require.NoError(t, pflag.Set("k8s.cli", cli))
			t.Cleanup(func() { require.NoError(t, pflag.Set("k8s.cli", original)) })
			ctx, err := shared.CreateScenarioContext(context.Background(), &godog.Scenario{})
			require.NoError(t, err)
			shared.Scenario(ctx).ID = "demo"
			dir := t.TempDir()
			recorder := platformsteps.NewAsciiCastRecorder("Verify ownership", dir)
			ctx = platformsteps.WithRecorder(ctx, recorder)
			recorder.BeginStep("Verify controller owner UID")

			err = platformsteps.ResourceControlledByClusterObjectSet(ctx, "configmap/settings-${SCENARIO_ID}", "cod-${SCENARIO_ID}-1")
			if tt.wantError {
				require.ErrorContains(t, err, "not controlled by ClusterObjectSet")
			} else {
				require.NoError(t, err)
			}
			recorder.CommitStep()
			require.NoError(t, recorder.Stop())
			content, err := os.ReadFile(filepath.Join(dir, "verify-ownership.cast"))
			require.NoError(t, err)
			if tt.wantError {
				require.NotContains(t, string(content), "Controller owner UID:")
				require.NotContains(t, string(content), "ClusterObjectSet UID:")
			} else {
				require.Contains(t, string(content), "Verify controller owner UID")
				require.Contains(t, string(content), "Controller owner UID: revision-uid")
				require.Contains(t, string(content), "ClusterObjectSet UID: revision-uid")
			}
		})
	}
}
