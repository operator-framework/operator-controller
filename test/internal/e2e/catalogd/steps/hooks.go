package steps

import (
	"context"
	"fmt"

	"github.com/cucumber/godog"

	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

// deploymentRestore records the original state of a deployment so it can be
// rolled back after a test that modifies deployment configuration.
type deploymentRestore struct {
	Name          string // deployment name
	Namespace     string
	containerName string   // container to patch (for env var restores)
	patchedArgs   bool     // true when container args were modified (for TLS profile patches)
	originalArgs  []string // original container args; may be nil if args were unset
	originalEnv   []string // original env vars as "NAME=VALUE" (for proxy patches)
}

type catalogContextKey struct{}
type catalogContext struct {
	proxy              *recordingProxy
	deploymentRestores []deploymentRestore
}

func catalogScenario(ctx context.Context) *catalogContext {
	return ctx.Value(catalogContextKey{}).(*catalogContext)
}

// RegisterHooks installs catalog-specific state and unconditional deployment restoration.
// Register it before shared hooks so modified deployments are restored before resource cleanup.
func RegisterHooks(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, catalogContextKey{}, &catalogContext{}), nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		sc := catalogScenario(ctx)
		// Stop the in-process recording proxy if one was started.
		if sc.proxy != nil {
			sc.proxy.stop()
		}
		// Restore any deployments that were modified during the scenario.  Runs
		// unconditionally (even on failure) to prevent a misconfigured deployment
		// from bleeding into subsequent scenarios.  Restored in LIFO order so that
		// multiple patches to the same deployment unwind to the true original.
		for i := len(sc.deploymentRestores) - 1; i >= 0; i-- {
			dr := sc.deploymentRestores[i]
			if dr.patchedArgs {
				if err2 := patchDeploymentArgs(dr.Namespace, dr.Name, dr.originalArgs); err2 != nil {
					shared.Log().Info("Error restoring deployment args", "name", dr.Name, "error", err2)
				} else if _, err2 := shared.K8sClient(ctx, "rollout", "status", "-n", dr.Namespace,
					fmt.Sprintf("deployment/%s", dr.Name), "--timeout=2m"); err2 != nil {
					shared.Log().Info("Timeout waiting for deployment rollout after restore", "name", dr.Name)
				}
			}
			if err2 := restoreDeployment(dr); err2 != nil {
				shared.Log().Info("Error restoring deployment env", "deployment", dr.Name, "namespace", dr.Namespace, "error", err2)
			}
		}

		return ctx, err
	})
}

// RegisterSteps registers catalogd HA, proxy, and TLS assertions.
func RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^(?i)the "([^"]+)" deployment is configured with custom TLS minimum version "([^"]+)"$`, ConfigureDeploymentWithCustomTLSVersion)
	sc.Step(`^(?i)the "([^"]+)" deployment is configured with custom TLS version "([^"]+)", ciphers "([^"]+)", and curves "([^"]+)"$`, ConfigureDeploymentWithCustomTLSFull)
	sc.Step(`^(?i)the "([^"]+)" metrics endpoint accepts a TLS 1\.3 connection$`, MetricsEndpointAcceptsTLS13)
	sc.Step(`^(?i)the "([^"]+)" metrics endpoint rejects a TLS 1\.2 connection$`, MetricsEndpointRejectsTLS12)
	sc.Step(`^(?i)the "([^"]+)" metrics endpoint negotiates cipher "([^"]+)" over TLS 1\.2$`, MetricsEndpointNegotiatesTLS12Cipher)
	sc.Step(`^(?i)the "([^"]+)" metrics endpoint rejects a TLS 1\.2 connection offering only cipher "([^"]+)"$`, MetricsEndpointRejectsTLS12ConnectionWithCipher)
	sc.Step(`^(?i)the "([^"]+)" metrics endpoint accepts a TLS 1\.2 connection with cipher "([^"]+)" and curve "([^"]+)"$`, MetricsEndpointAcceptsTLS12ConnectionWithCurve)
	sc.Step(`^(?i)the "([^"]+)" metrics endpoint rejects a TLS 1\.2 connection with cipher "([^"]+)" and only curve "([^"]+)"$`, MetricsEndpointRejectsTLS12ConnectionWithCurve)
	sc.Step(`^(?i)the "([^"]+)" metrics endpoint accepts a connection using only curve "([^"]+)"$`, MetricsEndpointAcceptsConnectionUsingCurve)
	sc.Step(`^(?i)the "([^"]+)" metrics endpoint rejects a connection using only curve "([^"]+)"$`, MetricsEndpointRejectsConnectionUsingOnlyCurve)
	sc.Step(`^(?i)the "([^"]+)" component is configured with HTTPS_PROXY "([^"]+)"$`, ConfigureDeploymentWithHTTPSProxy)
	sc.Step(`^(?i)the "([^"]+)" component is configured with HTTPS_PROXY pointing to a recording proxy$`, StartRecordingProxyAndConfigureDeployment)
	sc.Step(`^(?i)the recording proxy received a CONNECT request for the catalogd service$`, RecordingProxyReceivedCONNECTForCatalogd)
	sc.Step(`^(?i)the catalogd leader pod is force-deleted$`, CatalogdLeaderPodIsForceDeleted)
	sc.Step(`^(?i)a new catalogd leader is elected$`, NewCatalogdLeaderIsElected)
}
