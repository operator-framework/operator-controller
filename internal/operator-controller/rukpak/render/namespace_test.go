package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
)

func rv1WithAnnotations(pkg string, annotations map[string]string) *bundle.RegistryV1 {
	return &bundle.RegistryV1{
		PackageName: pkg,
		CSV:         csv.Builder().WithName("test-csv").WithAnnotations(annotations).Build(),
	}
}

func TestParseNamespaceTemplate(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    *corev1.Namespace
		expectError bool
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			expected:    nil,
		},
		{
			name:        "empty map",
			annotations: map[string]string{},
			expected:    nil,
		},
		{
			name:        "annotation absent",
			annotations: map[string]string{"some.other/annotation": "value"},
			expected:    nil,
		},
		{
			name:        "empty string value",
			annotations: map[string]string{AnnotationSuggestedNamespaceTemplate: ""},
			expected:    nil,
		},
		{
			name: "valid template with PSA labels",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{"metadata":{"labels":{"pod-security.kubernetes.io/enforce":"restricted"}}}`,
			},
			expected: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"pod-security.kubernetes.io/enforce": "restricted"},
				},
			},
		},
		{
			name: "valid template with annotations",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{"metadata":{"annotations":{"openshift.io/description":"Operator namespace"}}}`,
			},
			expected: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"openshift.io/description": "Operator namespace"},
				},
			},
		},
		{
			name:        "invalid JSON",
			annotations: map[string]string{AnnotationSuggestedNamespaceTemplate: `{"metadata": invalid json}`},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseNamespaceTemplate(tt.annotations)
			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveSystemManagedNamespace(t *testing.T) {
	tests := []struct {
		name         string
		annotations  map[string]string
		packageName  string
		wantName     string
		wantTemplate bool
	}{
		{
			name:         "suggested-namespace-template with name",
			annotations:  map[string]string{AnnotationSuggestedNamespaceTemplate: `{"metadata":{"name":"from-template","labels":{"pod-security.kubernetes.io/enforce":"privileged"}}}`},
			packageName:  "my-operator",
			wantName:     "from-template",
			wantTemplate: true,
		},
		{
			name:        "suggested-namespace without template",
			annotations: map[string]string{AnnotationSuggestedNamespace: "my-custom-ns"},
			packageName: "my-operator",
			wantName:    "my-custom-ns",
		},
		{
			name: "template takes priority over suggested-namespace",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{"metadata":{"name":"from-template"}}`,
				AnnotationSuggestedNamespace:         "from-annotation",
			},
			packageName:  "my-operator",
			wantName:     "from-template",
			wantTemplate: true,
		},
		{
			name:        "fallback to packageName-system",
			annotations: map[string]string{},
			packageName: "my-operator",
			wantName:    "my-operator-system",
		},
		{
			name:        "nil annotations fallback",
			annotations: nil,
			packageName: "my-operator",
			wantName:    "my-operator-system",
		},
		{
			name: "template without name falls back to suggested-namespace",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{"metadata":{"labels":{"foo":"bar"}}}`,
				AnnotationSuggestedNamespace:         "from-annotation",
			},
			packageName:  "my-operator",
			wantName:     "from-annotation",
			wantTemplate: true,
		},
		{
			name:         "template without name and no suggested-namespace falls back to convention",
			annotations:  map[string]string{AnnotationSuggestedNamespaceTemplate: `{"metadata":{"labels":{"foo":"bar"}}}`},
			packageName:  "my-operator",
			wantName:     "my-operator-system",
			wantTemplate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, template, err := resolveSystemManagedNamespace(rv1WithAnnotations(tt.packageName, tt.annotations))
			require.NoError(t, err)
			require.Equal(t, tt.wantName, name)
			if tt.wantTemplate {
				require.NotNil(t, template)
			} else {
				require.Nil(t, template)
			}
		})
	}
}

func TestResolveSystemManagedNamespace_InvalidTemplate(t *testing.T) {
	_, _, err := resolveSystemManagedNamespace(rv1WithAnnotations("pkg", map[string]string{
		AnnotationSuggestedNamespaceTemplate: `{invalid json`,
	}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse namespace template")
}

func TestResolveSystemManagedNamespace_Validation(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		packageName string
		expectErr   bool
		errContains string
	}{
		{
			name:        "rejects uppercase characters in suggested-namespace",
			annotations: map[string]string{AnnotationSuggestedNamespace: "Invalid-NS"},
			packageName: "pkg",
			expectErr:   true,
			errContains: "not a valid DNS1123 label",
		},
		{
			name:        "rejects name exceeding 63 characters",
			annotations: map[string]string{AnnotationSuggestedNamespace: "a234567890123456789012345678901234567890123456789012345678901234"},
			packageName: "pkg",
			expectErr:   true,
			errContains: "exceeds 63 characters",
		},
		{
			name:        "rejects name with dots",
			annotations: map[string]string{AnnotationSuggestedNamespace: "my.namespace"},
			packageName: "pkg",
			expectErr:   true,
			errContains: "not a valid DNS1123 label",
		},
		{
			name:        "accepts valid fallback name",
			annotations: nil,
			packageName: "my-package",
		},
		{
			name:        "accepts valid suggested-namespace",
			annotations: map[string]string{AnnotationSuggestedNamespace: "valid-ns-123"},
			packageName: "pkg",
		},
		{
			name:        "rejects invalid name from template",
			annotations: map[string]string{AnnotationSuggestedNamespaceTemplate: `{"metadata":{"name":"INVALID"}}`},
			packageName: "pkg",
			expectErr:   true,
			errContains: "not a valid DNS1123 label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := resolveSystemManagedNamespace(rv1WithAnnotations(tt.packageName, tt.annotations))
			if tt.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBuildNamespaceObject(t *testing.T) {
	tests := []struct {
		name     string
		nsName   string
		template *corev1.Namespace
		validate func(t *testing.T, obj map[string]interface{})
	}{
		{
			name:   "with template labels",
			nsName: "my-ns",
			template: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"pod-security.kubernetes.io/enforce": "restricted"},
				},
			},
			validate: func(t *testing.T, obj map[string]interface{}) {
				assert.Equal(t, "v1", obj["apiVersion"])
				assert.Equal(t, "Namespace", obj["kind"])
				metadata := obj["metadata"].(map[string]interface{})
				assert.Equal(t, "my-ns", metadata["name"])
				labels := metadata["labels"].(map[string]interface{})
				assert.Equal(t, "restricted", labels["pod-security.kubernetes.io/enforce"])
			},
		},
		{
			name:     "nil template",
			nsName:   "my-ns",
			template: nil,
			validate: func(t *testing.T, obj map[string]interface{}) {
				metadata := obj["metadata"].(map[string]interface{})
				assert.Equal(t, "my-ns", metadata["name"])
				_, hasLabels := metadata["labels"]
				assert.False(t, hasLabels)
			},
		},
		{
			name:   "template name is overridden",
			nsName: "override",
			template: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "template-name"},
			},
			validate: func(t *testing.T, obj map[string]interface{}) {
				metadata := obj["metadata"].(map[string]interface{})
				assert.Equal(t, "override", metadata["name"])
			},
		},
		{
			name:   "strips empty spec and status",
			nsName: "my-ns",
			validate: func(t *testing.T, obj map[string]interface{}) {
				_, hasSpec := obj["spec"]
				_, hasStatus := obj["status"]
				assert.False(t, hasSpec)
				assert.False(t, hasStatus)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildNamespaceObject(tt.nsName, tt.template)
			require.NoError(t, err)
			tt.validate(t, result.(*unstructured.Unstructured).Object)
		})
	}
}
