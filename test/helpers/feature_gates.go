// Package utils provides helper functions for e2e tests, including
// feature gate detection and validation utilities.
package utils

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/component-base/featuregate"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catdfeatures "github.com/operator-framework/operator-controller/internal/catalogd/features"
	opconfeatures "github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

var (
	featureGateStatus     map[string]bool
	featureGateStatusOnce sync.Once
)

const (
	fgPrefix = "--feature-gates="
)

// SkipIfFeatureGateDisabled skips the test if the specified feature gate is disabled.
// It queries the OLM deployments to detect feature gate settings and falls back to
// programmatic defaults if the feature gate is not explicitly configured.
func SkipIfFeatureGateDisabled(t *testing.T, fg string) {
	if !isFeatureGateEnabled(t, fg) {
		t.Skipf("Feature-gate %q disabled", fg)
	}
}

func isFeatureGateEnabled(t *testing.T, fg string) bool {
	gatherFeatureGates(t)
	enabled, ok := featureGateStatus[fg]
	if ok {
		return enabled
	}

	// Not found (i.e. not explicitly set), so we need to find the programmed default.
	// Because feature-gates are organized by catd/opcon, we need to check each individually.
	// To avoid a panic, we need to check if it's a known gate first.
	mfgs := []featuregate.MutableFeatureGate{
		catdfeatures.CatalogdFeatureGate,
		opconfeatures.OperatorControllerFeatureGate,
	}
	f := featuregate.Feature(fg)
	for _, mfg := range mfgs {
		known := mfg.GetAll()
		if _, ok := known[f]; ok {
			e := mfg.Enabled(f)
			t.Logf("Feature-gate %q not found in arguments, defaulting to %v", fg, e)
			return e
		}
	}

	t.Fatalf("Unknown feature-gate: %q", fg)
	return false // unreachable, but required for compilation
}

func processFeatureGate(t *testing.T, featureGateValue string) {
	fgvs := strings.Split(featureGateValue, ",")
	for _, fg := range fgvs {
		v := strings.Split(fg, "=")
		require.Len(t, v, 2, "invalid feature-gate format: %q (expected name=value)", fg)
		switch v[1] {
		case "true":
			featureGateStatus[v[0]] = true
			t.Logf("Feature-gate %q enabled", v[0])
		case "false":
			featureGateStatus[v[0]] = false
			t.Logf("Feature-gate %q disabled", v[0])
		default:
			t.Fatalf("invalid feature-gate value: %q (expected true or false)", fg)
		}
	}
}

func gatherFeatureGatesFromDeployment(t *testing.T, dep *appsv1.Deployment) {
	for _, con := range dep.Spec.Template.Spec.Containers {
		for _, arg := range con.Args {
			if strings.HasPrefix(arg, fgPrefix) {
				processFeatureGate(t, strings.TrimPrefix(arg, fgPrefix))
			}
		}
	}
}

func gatherFeatureGates(t *testing.T) {
	featureGateStatusOnce.Do(func() {
		featureGateStatus = make(map[string]bool)

		depList := &appsv1.DeploymentList{}
		err := c.List(t.Context(), depList, client.MatchingLabels{
			"app.kubernetes.io/part-of": "olm",
		})
		require.NoError(t, err)
		require.Len(t, depList.Items, 2)

		for _, d := range depList.Items {
			gatherFeatureGatesFromDeployment(t, &d)
		}
	})
}
