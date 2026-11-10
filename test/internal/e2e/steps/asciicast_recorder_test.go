package steps_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	platformsteps "github.com/operator-framework/operator-controller/test/internal/e2e/platform/steps"
	"github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func TestWithRecorder(t *testing.T) {
	parent := steps.WithCommandRecorder(context.Background(), &platformsteps.AsciiCastRecorder{})
	recorder := &platformsteps.AsciiCastRecorder{}

	ctx := platformsteps.WithRecorder(parent, recorder)

	require.Same(t, recorder, platformsteps.RecorderFromContext(ctx))
	require.Same(t, recorder, steps.CommandRecorderFromContext(ctx))
	require.NotSame(t, recorder, steps.CommandRecorderFromContext(parent))
	require.Nil(t, platformsteps.RecorderFromContext(parent))
}

func TestSharedCommandsReachPlatformRecorder(t *testing.T) {
	cli, err := exec.LookPath("echo")
	require.NoError(t, err)
	original := pflag.Lookup("k8s.cli").Value.String()
	require.NoError(t, pflag.Set("k8s.cli", cli))
	t.Cleanup(func() { require.NoError(t, pflag.Set("k8s.cli", original)) })
	dir := t.TempDir()
	recorder := platformsteps.NewAsciiCastRecorder("Shared command recording", dir)
	ctx := platformsteps.WithRecorder(context.Background(), recorder)
	recorder.BeginStep("List pods")
	_, err = steps.K8sClient(ctx, "get", "pods")
	require.NoError(t, err)
	recorder.CommitStep()
	require.NoError(t, recorder.Stop())
	content, err := os.ReadFile(filepath.Join(dir, "shared-command-recording.cast"))
	require.NoError(t, err)
	require.Contains(t, string(content), "get pods")
	require.Contains(t, string(content), "List pods")
	require.Contains(t, string(content), `"version":2`)
	require.Nil(t, steps.CommandRecorderFromContext(context.Background()))
}
