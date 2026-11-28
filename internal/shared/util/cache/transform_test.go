package cache

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const lastAppliedAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

func newConfigMap(annotations map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "cm",
			Namespace:   "ns",
			Annotations: annotations,
			ManagedFields: []metav1.ManagedFieldsEntry{
				{Manager: "kubectl"},
			},
		},
	}
}

func TestStripAnnotations(t *testing.T) {
	tests := []struct {
		name            string
		annotations     map[string]string
		wantAnnotations map[string]string
	}{
		{
			name:            "nil annotations are left alone",
			annotations:     nil,
			wantAnnotations: nil,
		},
		{
			name:            "unrelated annotations are preserved",
			annotations:     map[string]string{"foo": "bar"},
			wantAnnotations: map[string]string{"foo": "bar"},
		},
		{
			name:            "last-applied-configuration is removed",
			annotations:     map[string]string{lastAppliedAnnotation: "{}", "foo": "bar"},
			wantAnnotations: map[string]string{"foo": "bar"},
		},
		{
			name:            "annotations map is cleared when last-applied-configuration was the only entry",
			annotations:     map[string]string{lastAppliedAnnotation: "{}"},
			wantAnnotations: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := newConfigMap(tc.annotations)
			got, err := StripAnnotations()(obj)
			require.NoError(t, err)
			require.Same(t, obj, got)
			require.Equal(t, tc.wantAnnotations, obj.GetAnnotations())
			require.NotEmpty(t, obj.GetManagedFields(), "StripAnnotations must not strip managed fields")
		})
	}
}

func TestStripAnnotations_DoesNotMutateSharedMap(t *testing.T) {
	shared := map[string]string{lastAppliedAnnotation: "{}", "foo": "bar"}
	obj := newConfigMap(shared)

	_, err := StripAnnotations()(obj)
	require.NoError(t, err)

	require.Equal(t, map[string]string{lastAppliedAnnotation: "{}", "foo": "bar"}, shared,
		"the original annotations map must not be modified")
}

func TestStripAnnotations_NonClientObject(t *testing.T) {
	in := "not a client.Object"
	got, err := StripAnnotations()(in)
	require.NoError(t, err)
	require.Equal(t, in, got)
}

func TestStripManagedFieldsAndAnnotations(t *testing.T) {
	obj := newConfigMap(map[string]string{lastAppliedAnnotation: "{}", "foo": "bar"})

	got, err := StripManagedFieldsAndAnnotations()(obj)
	require.NoError(t, err)
	require.Same(t, obj, got)
	require.Empty(t, obj.GetManagedFields())
	require.Equal(t, map[string]string{"foo": "bar"}, obj.GetAnnotations())
}

func TestApplyStripAnnotationsTransform(t *testing.T) {
	obj := newConfigMap(map[string]string{lastAppliedAnnotation: "{}", "foo": "bar"})

	require.NoError(t, ApplyStripAnnotationsTransform(obj))
	require.Equal(t, map[string]string{"foo": "bar"}, obj.GetAnnotations())
	require.NotEmpty(t, obj.GetManagedFields())
}
