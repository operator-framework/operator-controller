package k8s

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestCheckForUnexpectedFieldChange(t *testing.T) {
	tests := []struct {
		name     string
		a        ocv1.ClusterExtension
		b        ocv1.ClusterExtension
		expected bool
	}{
		{
			name: "no changes",
			a: ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value"},
					Finalizers:  []string{"finalizer1"},
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{Type: "Ready", Status: metav1.ConditionTrue},
					},
				},
			},
			b: ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value"},
					Finalizers:  []string{"finalizer2"}, // Different finalizer should not trigger change
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{Type: "Ready", Status: metav1.ConditionFalse}, // Different status should not trigger change
					},
				},
			},
			expected: false,
		},
		{
			name: "annotation changed",
			a: ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value1"},
					Labels:      map[string]string{"label": "value"},
				},
				Spec: ocv1.ClusterExtensionSpec{},
			},
			b: ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value2"},
					Labels:      map[string]string{"label": "value"},
				},
				Spec: ocv1.ClusterExtensionSpec{},
			},
			expected: true,
		},
		{
			name: "label changed",
			a: ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value1"},
				},
				Spec: ocv1.ClusterExtensionSpec{},
			},
			b: ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value2"},
				},
				Spec: ocv1.ClusterExtensionSpec{},
			},
			expected: true,
		},
		{
			name: "spec changed",
			a: ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value"},
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
					},
				},
			},
			b: ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value"},
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Image",
					},
				},
			},
			expected: true,
		},
		{
			name: "status changed but annotations, labels, spec same",
			a: ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value"},
				},
				Spec: ocv1.ClusterExtensionSpec{},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{Type: "Ready", Status: metav1.ConditionTrue},
					},
				},
			},
			b: ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value"},
				},
				Spec: ocv1.ClusterExtensionSpec{},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{Type: "Ready", Status: metav1.ConditionFalse},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckForUnexpectedFieldChange(&tt.a, &tt.b)
			if result != tt.expected {
				t.Errorf("CheckForUnexpectedFieldChange() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCheckForUnexpectedFieldChangeWithClusterCatalog(t *testing.T) {
	tests := []struct {
		name     string
		a        ocv1.ClusterCatalog
		b        ocv1.ClusterCatalog
		expected bool
	}{
		{
			name: "no changes",
			a: ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value"},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: "Image",
					},
				},
			},
			b: ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value"},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: "Image",
					},
				},
			},
			expected: false,
		},
		{
			name: "spec changed",
			a: ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value"},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: "Image",
					},
				},
			},
			b: ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
					Labels:      map[string]string{"label": "value"},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: "Git",
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckForUnexpectedFieldChange(&tt.a, &tt.b)
			if result != tt.expected {
				t.Errorf("CheckForUnexpectedFieldChange() = %v, want %v", result, tt.expected)
			}
		})
	}
}
