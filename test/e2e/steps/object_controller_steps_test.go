package steps

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

func TestObjectControllerScenarioSelection(t *testing.T) {
	originalGates, originalNamespaces := featureGates, componentNamespaces
	originalNamespace, originalOnly := olmNamespace, objectControllerOnly
	t.Cleanup(func() {
		featureGates, componentNamespaces = originalGates, originalNamespaces
		olmNamespace, objectControllerOnly = originalNamespace, originalOnly
	})
	for _, tc := range []struct {
		name          string
		objectPresent bool
		operator      bool
		boxcutter     bool
		standalone    bool
		wantSkip      bool
	}{
		{name: "standalone", objectPresent: true, standalone: true},
		{name: "standard", operator: true, wantSkip: true},
		{name: "integrated", objectPresent: true, operator: true, boxcutter: true},
		{name: "unready deployment must run readiness check", objectPresent: true},
		{name: "missing integrated controller must fail readiness", operator: true, boxcutter: true},
		{name: "missing standalone controller must fail readiness", standalone: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			featureGates = maps.Clone(originalGates)
			componentNamespaces = map[string]string{}
			objectControllerOnly = tc.standalone
			deployments := map[string]*appsv1.Deployment{}
			if tc.objectPresent {
				// Discovery must enable scenarios even before the Deployment is ready.
				deployments["object-controller"] = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "objects"}}
			}
			if tc.operator {
				deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "extensions"}}
				deployment.Spec.Template.Spec.Containers = []corev1.Container{{Name: "manager", Args: []string{"--feature-gates=BoxcutterRuntime=false"}}}
				if tc.boxcutter {
					deployment.Spec.Template.Spec.Containers[0].Args = []string{"--feature-gates=BoxcutterRuntime=true"}
				}
				deployments["operator-controller"] = deployment
			}
			configureControllerFeatures(deployments)
			require.Equal(t, tc.objectPresent, featureGates[objectControllerFeature])
			require.Equal(t, tc.boxcutter, featureGates[features.BoxcutterRuntime])
			if tc.objectPresent {
				require.Equal(t, "objects", namespaceForComponent("object-controller"))
			}
			scenario := &godog.Scenario{}
			require.NoError(t, json.Unmarshal([]byte(`{"tags":[{"name":"@ObjectController"}]}`), scenario))
			_, err := CheckFeatureTags(context.Background(), scenario)
			if tc.wantSkip {
				require.ErrorIs(t, err, godog.ErrSkip)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
