package applier

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

func TestBuildClusterObjectDeployment(t *testing.T) {
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ext",
		},
		Spec: ocv1.ClusterExtensionSpec{
			ProgressDeadlineMinutes: 15,
		},
	}

	// Create a valid unstructured object with proper GVK
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name": "test-cm",
			},
		},
	}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))

	objects := []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
		*ocv1ac.ClusterObjectSetObject().WithObject(*obj),
	}

	annotations := map[string]string{
		"test-key": "test-value",
	}

	collisionProtection := ocv1.CollisionProtectionPrevent

	result := buildClusterObjectDeployment(objects, ext, annotations, collisionProtection)

	require.NotNil(t, result)
	assert.Equal(t, "ext", *result.Name)
	assert.Equal(t, "test-value", result.Annotations["test-key"])
	assert.Equal(t, ocv1.ClusterExtensionKind, result.Labels[labels.OwnerKindKey])
	assert.Equal(t, "ext", result.Labels[labels.OwnerNameKey])

	require.NotNil(t, result.Spec)
	require.NotNil(t, result.Spec.Template)
	require.NotNil(t, result.Spec.Template.Spec)

	templateSpec := result.Spec.Template.Spec
	assert.Len(t, templateSpec.Phases, 1)
	assert.Equal(t, ocv1.CollisionProtectionPrevent, *templateSpec.CollisionProtection)
	assert.Equal(t, int32(15), *templateSpec.ProgressDeadlineMinutes)
	assert.NotEmpty(t, templateSpec.ProgressionProbes)
}
