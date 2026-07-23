package applier

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
			expectError: false,
		},
		{
			name:        "empty map",
			annotations: map[string]string{},
			expected:    nil,
			expectError: false,
		},
		{
			name: "annotation absent",
			annotations: map[string]string{
				"some.other/annotation": "value",
			},
			expected:    nil,
			expectError: false,
		},
		{
			name: "empty string value",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: "",
			},
			expected:    nil,
			expectError: false,
		},
		{
			name: "valid template with PSA labels",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{
					"metadata": {
						"labels": {
							"pod-security.kubernetes.io/enforce": "restricted",
							"pod-security.kubernetes.io/audit": "restricted",
							"pod-security.kubernetes.io/warn": "restricted"
						}
					}
				}`,
			},
			expected: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"pod-security.kubernetes.io/enforce": "restricted",
						"pod-security.kubernetes.io/audit":   "restricted",
						"pod-security.kubernetes.io/warn":    "restricted",
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid template with annotations",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{
					"metadata": {
						"annotations": {
							"openshift.io/description": "Operator namespace",
							"openshift.io/display-name": "My Operator"
						}
					}
				}`,
			},
			expected: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"openshift.io/description":  "Operator namespace",
						"openshift.io/display-name": "My Operator",
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid template with both labels and annotations",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{
					"metadata": {
						"labels": {
							"pod-security.kubernetes.io/enforce": "baseline"
						},
						"annotations": {
							"openshift.io/description": "Operator namespace"
						}
					}
				}`,
			},
			expected: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"pod-security.kubernetes.io/enforce": "baseline",
					},
					Annotations: map[string]string{
						"openshift.io/description": "Operator namespace",
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid JSON",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{"metadata": invalid json}`,
			},
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseNamespaceTemplate(tt.annotations)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
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
					Labels: map[string]string{
						"pod-security.kubernetes.io/enforce": "restricted",
						"pod-security.kubernetes.io/audit":   "restricted",
						"pod-security.kubernetes.io/warn":    "restricted",
					},
				},
			},
			validate: func(t *testing.T, obj map[string]interface{}) {
				assert.Equal(t, "v1", obj["apiVersion"])
				assert.Equal(t, "Namespace", obj["kind"])

				metadata, ok := obj["metadata"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "my-ns", metadata["name"])

				labels, ok := metadata["labels"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "restricted", labels["pod-security.kubernetes.io/enforce"])
				assert.Equal(t, "restricted", labels["pod-security.kubernetes.io/audit"])
				assert.Equal(t, "restricted", labels["pod-security.kubernetes.io/warn"])
			},
		},
		{
			name:     "nil template",
			nsName:   "my-ns",
			template: nil,
			validate: func(t *testing.T, obj map[string]interface{}) {
				assert.Equal(t, "v1", obj["apiVersion"])
				assert.Equal(t, "Namespace", obj["kind"])

				metadata, ok := obj["metadata"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "my-ns", metadata["name"])

				_, hasLabels := metadata["labels"]
				assert.False(t, hasLabels)
			},
		},
		{
			name:   "template name is overridden",
			nsName: "override",
			template: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "template-name",
				},
			},
			validate: func(t *testing.T, obj map[string]interface{}) {
				metadata, ok := obj["metadata"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "override", metadata["name"])
			},
		},
		{
			name:   "with template annotations",
			nsName: "my-ns",
			template: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"openshift.io/description":  "Operator namespace",
						"openshift.io/display-name": "My Operator",
					},
				},
			},
			validate: func(t *testing.T, obj map[string]interface{}) {
				assert.Equal(t, "v1", obj["apiVersion"])
				assert.Equal(t, "Namespace", obj["kind"])

				metadata, ok := obj["metadata"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "my-ns", metadata["name"])

				annotations, ok := metadata["annotations"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "Operator namespace", annotations["openshift.io/description"])
				assert.Equal(t, "My Operator", annotations["openshift.io/display-name"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildNamespaceObject(tt.nsName, tt.template)
			require.NoError(t, err)
			tt.validate(t, result.Object)
		})
	}
}

func TestResolveNamespaceName(t *testing.T) {
	tests := []struct {
		name           string
		csvAnnotations map[string]string
		packageName    string
		wantName       string
		wantTemplate   bool
	}{
		{
			name: "suggested-namespace-template with name",
			csvAnnotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{"metadata":{"name":"from-template","labels":{"pod-security.kubernetes.io/enforce":"privileged"}}}`,
			},
			packageName:  "my-operator",
			wantName:     "from-template",
			wantTemplate: true,
		},
		{
			name: "suggested-namespace without template",
			csvAnnotations: map[string]string{
				AnnotationSuggestedNamespace: "my-custom-ns",
			},
			packageName:  "my-operator",
			wantName:     "my-custom-ns",
			wantTemplate: false,
		},
		{
			name: "template takes priority over suggested-namespace",
			csvAnnotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{"metadata":{"name":"from-template"}}`,
				AnnotationSuggestedNamespace:         "from-annotation",
			},
			packageName:  "my-operator",
			wantName:     "from-template",
			wantTemplate: true,
		},
		{
			name:           "fallback to packageName-system",
			csvAnnotations: map[string]string{},
			packageName:    "my-operator",
			wantName:       "my-operator-system",
			wantTemplate:   false,
		},
		{
			name:           "nil annotations fallback",
			csvAnnotations: nil,
			packageName:    "my-operator",
			wantName:       "my-operator-system",
			wantTemplate:   false,
		},
		{
			name: "template without name falls back to suggested-namespace",
			csvAnnotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{"metadata":{"labels":{"foo":"bar"}}}`,
				AnnotationSuggestedNamespace:         "from-annotation",
			},
			packageName:  "my-operator",
			wantName:     "from-annotation",
			wantTemplate: true,
		},
		{
			name: "template without name and no suggested-namespace falls back to convention",
			csvAnnotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{"metadata":{"labels":{"foo":"bar"}}}`,
			},
			packageName:  "my-operator",
			wantName:     "my-operator-system",
			wantTemplate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, template, err := ResolveNamespaceName(tt.csvAnnotations, tt.packageName)
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

func TestResolveNamespaceName_InvalidTemplate(t *testing.T) {
	_, _, err := ResolveNamespaceName(map[string]string{
		AnnotationSuggestedNamespaceTemplate: `{invalid json`,
	}, "pkg")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse namespace template")
}

func TestResolveNamespaceName_Validation(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		packageName string
		expectErr   bool
		errContains string
	}{
		{
			name: "rejects uppercase characters in suggested-namespace",
			annotations: map[string]string{
				AnnotationSuggestedNamespace: "Invalid-NS",
			},
			packageName: "pkg",
			expectErr:   true,
			errContains: "not a valid DNS1123 label",
		},
		{
			name: "rejects name exceeding 63 characters",
			annotations: map[string]string{
				AnnotationSuggestedNamespace: "a234567890123456789012345678901234567890123456789012345678901234",
			},
			packageName: "pkg",
			expectErr:   true,
			errContains: "exceeds 63 characters",
		},
		{
			name: "rejects name with dots",
			annotations: map[string]string{
				AnnotationSuggestedNamespace: "my.namespace",
			},
			packageName: "pkg",
			expectErr:   true,
			errContains: "not a valid DNS1123 label",
		},
		{
			name: "rejects name starting with hyphen",
			annotations: map[string]string{
				AnnotationSuggestedNamespace: "-invalid",
			},
			packageName: "pkg",
			expectErr:   true,
			errContains: "not a valid DNS1123 label",
		},
		{
			name:        "accepts valid fallback name",
			annotations: nil,
			packageName: "my-package",
			expectErr:   false,
		},
		{
			name: "accepts valid suggested-namespace",
			annotations: map[string]string{
				AnnotationSuggestedNamespace: "valid-ns-123",
			},
			packageName: "pkg",
			expectErr:   false,
		},
		{
			name: "rejects invalid name from template",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{"metadata":{"name":"INVALID"}}`,
			},
			packageName: "pkg",
			expectErr:   true,
			errContains: "not a valid DNS1123 label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ResolveNamespaceName(tt.annotations, tt.packageName)
			if tt.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBuildNamespaceObject_ReturnsError(t *testing.T) {
	// BuildNamespaceObject with valid input should not error
	_, err := BuildNamespaceObject("valid-ns", nil)
	require.NoError(t, err)

	// With template
	template := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"key": "value"},
		},
	}
	obj, err := BuildNamespaceObject("valid-ns", template)
	require.NoError(t, err)
	require.Equal(t, "valid-ns", obj.GetName())
	require.Equal(t, "value", obj.GetLabels()["key"])
}
