package migration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestStripResource(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":              "test-deployment",
				"namespace":         "test-ns",
				"uid":               "abc-123",
				"resourceVersion":   "12345",
				"generation":        int64(3),
				"creationTimestamp": "2024-01-01T00:00:00Z",
				"ownerReferences":   []interface{}{},
				"managedFields":     []interface{}{},
				"labels": map[string]interface{}{
					"app": "test",
				},
				"annotations": map[string]interface{}{
					"custom-annotation":                                "keep-this",
					"kubectl.kubernetes.io/last-applied":               "remove-this",
					"deployment.kubernetes.io/revision":                "remove-this",
					"olm.operatorframework.io/installed-alongside-abc": "remove-this",
				},
			},
			"spec": map[string]interface{}{
				"replicas": int64(1),
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]interface{}{
							"keep-me":                            "yes",
							"kubectl.kubernetes.io/restartedAt":  "remove-nested",
							"deployment.kubernetes.io/something": "remove-nested",
						},
					},
				},
			},
			"status": map[string]interface{}{
				"replicas": int64(1),
			},
		},
	}

	stripped := stripResource(obj)

	// Should keep essential fields
	assert.Equal(t, "apps/v1", stripped.GetAPIVersion())
	assert.Equal(t, "Deployment", stripped.GetKind())
	assert.Equal(t, "test-deployment", stripped.GetName())
	assert.Equal(t, "test-ns", stripped.GetNamespace())

	// Should keep labels
	assert.Equal(t, map[string]string{"app": "test"}, stripped.GetLabels())

	// Should filter annotations
	annotations := stripped.GetAnnotations()
	assert.Contains(t, annotations, "custom-annotation")
	assert.NotContains(t, annotations, "kubectl.kubernetes.io/last-applied")
	assert.NotContains(t, annotations, "deployment.kubernetes.io/revision")

	// Should not have status, uid, resourceVersion, etc.
	_, hasStatus := stripped.Object["status"]
	assert.False(t, hasStatus)
	assert.Empty(t, stripped.GetUID())
	assert.Empty(t, stripped.GetResourceVersion())
	assert.Empty(t, stripped.GetOwnerReferences())

	// Should strip nested annotations
	nestedAnnotations, found, _ := unstructured.NestedMap(stripped.Object, "spec", "template", "metadata", "annotations")
	assert.True(t, found)
	assert.Contains(t, nestedAnnotations, "keep-me")
	assert.NotContains(t, nestedAnnotations, "kubectl.kubernetes.io/restartedAt")
	assert.NotContains(t, nestedAnnotations, "deployment.kubernetes.io/something")
}

func TestStripResourceClusterScoped(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name": "test-role",
			},
			"rules": []interface{}{
				map[string]interface{}{
					"apiGroups": []interface{}{""},
					"resources": []interface{}{"pods"},
					"verbs":     []interface{}{"get", "list"},
				},
			},
		},
	}

	stripped := stripResource(obj)
	assert.Equal(t, "ClusterRole", stripped.GetKind())
	assert.Equal(t, "test-role", stripped.GetName())
	assert.Empty(t, stripped.GetNamespace())
	assert.NotNil(t, stripped.Object["rules"])
}

func TestFilterAnnotations(t *testing.T) {
	annotations := map[string]string{
		"custom":                                         "keep",
		"another.io/annotation":                          "keep",
		"kubectl.kubernetes.io/last-applied":             "remove",
		"deployment.kubernetes.io/revision":              "remove",
		"olm.operatorframework.io/installed-alongside-x": "remove",
	}

	filtered := filterAnnotations(annotations)
	assert.Len(t, filtered, 2)
	assert.Contains(t, filtered, "custom")
	assert.Contains(t, filtered, "another.io/annotation")
}

func TestOptionsApplyDefaults(t *testing.T) {
	opts := Options{
		SubscriptionName:      "my-operator",
		SubscriptionNamespace: "operators",
	}
	opts.ApplyDefaults()

	assert.Equal(t, "my-operator", opts.ClusterExtensionName)
	assert.Equal(t, "operators", opts.InstallNamespace)
	assert.Equal(t, "my-operator-installer", opts.ServiceAccountName())
}

func TestOptionsApplyDefaultsWithOverrides(t *testing.T) {
	opts := Options{
		SubscriptionName:      "my-operator",
		SubscriptionNamespace: "operators",
		ClusterExtensionName:  "custom-name",
		InstallNamespace:      "custom-ns",
	}
	opts.ApplyDefaults()

	assert.Equal(t, "custom-name", opts.ClusterExtensionName)
	assert.Equal(t, "custom-ns", opts.InstallNamespace)
	assert.Equal(t, "custom-name-installer", opts.ServiceAccountName())
}
