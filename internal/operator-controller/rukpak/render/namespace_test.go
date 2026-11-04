package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
			expected:    &corev1.Namespace{},
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
			name: "valid YAML template",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `apiVersion: v1
kind: Namespace
metadata:
  name: template-ns
  labels:
    pod-security.kubernetes.io/enforce: restricted
  annotations:
    example.com/description: Operator namespace
  finalizers: [example.com/cleanup]
spec:
  finalizers: [kubernetes]
status:
  phase: Active`,
			},
			expected: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
				ObjectMeta: metav1.ObjectMeta{
					Name:        "template-ns",
					Labels:      map[string]string{"pod-security.kubernetes.io/enforce": "restricted"},
					Annotations: map[string]string{"example.com/description": "Operator namespace"},
					Finalizers:  []string{"example.com/cleanup"},
				},
				Spec:   corev1.NamespaceSpec{Finalizers: []corev1.FinalizerName{corev1.FinalizerKubernetes}},
				Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
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
		name        string
		annotations map[string]string
		packageName string
		wantName    string
		expectError bool
	}{
		{
			name:        "suggested-namespace-template with name",
			annotations: map[string]string{AnnotationSuggestedNamespaceTemplate: `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"from-template","labels":{"pod-security.kubernetes.io/enforce":"privileged"}}}`},
			packageName: "my-operator",
			wantName:    "from-template",
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
				AnnotationSuggestedNamespaceTemplate: `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"from-template"}}`,
				AnnotationSuggestedNamespace:         "from-annotation",
			},
			packageName: "my-operator",
			wantName:    "from-template",
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
			name: "template without name does not fall back to suggested-namespace",
			annotations: map[string]string{
				AnnotationSuggestedNamespaceTemplate: `{"apiVersion":"v1","kind":"Namespace","metadata":{"labels":{"foo":"bar"}}}`,
				AnnotationSuggestedNamespace:         "Invalid_NS",
			},
			packageName: "my-operator",
			expectError: true,
		},
		{
			name:        "template without name produces an error",
			annotations: map[string]string{AnnotationSuggestedNamespaceTemplate: `{"apiVersion":"v1","kind":"Namespace","metadata":{"labels":{"foo":"bar"}}}`},
			packageName: "my-operator",
			expectError: true,
		},
		{
			name: "empty suggested-namespace produces an error",
			annotations: map[string]string{
				AnnotationSuggestedNamespace: "",
			},
			packageName: "my-operator",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, template, err := resolveSystemManagedNamespace(rv1WithAnnotations(tt.packageName, tt.annotations))
			if tt.expectError {
				require.ErrorContains(t, err, "namespace name is empty")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantName, name)
			require.NotNil(t, template)
			require.Equal(t, name, template.Name)
			require.Equal(t, metav1.TypeMeta{Kind: "Namespace", APIVersion: "v1"}, template.TypeMeta)
		})
	}
}

func TestResolveSystemManagedNamespace_PreservesTemplate(t *testing.T) {
	wantNamespace := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "template-ns",
			Labels:      map[string]string{"pod-security.kubernetes.io/enforce": "restricted"},
			Annotations: map[string]string{"example.com/description": "Operator namespace"},
			Finalizers:  []string{"example.com/cleanup"},
		},
		Spec:   corev1.NamespaceSpec{Finalizers: []corev1.FinalizerName{corev1.FinalizerKubernetes}},
		Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
	templateJSON, err := json.Marshal(wantNamespace)
	require.NoError(t, err)

	name, namespace, err := resolveSystemManagedNamespace(rv1WithAnnotations("my-operator", map[string]string{
		AnnotationSuggestedNamespaceTemplate: string(templateJSON),
	}))
	require.NoError(t, err)
	require.Equal(t, wantNamespace.Name, name)
	require.Equal(t, wantNamespace, namespace)
}

func TestResolveSystemManagedNamespace_InvalidTemplate(t *testing.T) {
	_, _, err := resolveSystemManagedNamespace(rv1WithAnnotations("pkg", map[string]string{
		AnnotationSuggestedNamespaceTemplate: `{invalid json`,
		AnnotationSuggestedNamespace:         "must-not-fall-back",
	}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse CSV namespace template")
	require.Contains(t, err.Error(), AnnotationSuggestedNamespaceTemplate)
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
			name:        "rejects empty template instead of falling back",
			annotations: map[string]string{AnnotationSuggestedNamespaceTemplate: `{"apiVersion":"v1","kind":"Namespace"}`, AnnotationSuggestedNamespace: "must-not-fall-back"},
			packageName: "pkg",
			expectErr:   true,
			errContains: "namespace name is empty",
		},
		{
			name:        "rejects empty suggested-namespace instead of falling back",
			annotations: map[string]string{AnnotationSuggestedNamespace: ""},
			packageName: "pkg",
			expectErr:   true,
			errContains: "namespace name is empty",
		},
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
			annotations: map[string]string{AnnotationSuggestedNamespaceTemplate: `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"INVALID"}}`},
			packageName: "pkg",
			expectErr:   true,
			errContains: "not a valid DNS1123 label",
		},
		{
			name:        "rejects invalid version from template",
			annotations: map[string]string{AnnotationSuggestedNamespaceTemplate: `{"apiVersion":"v2","kind":"Namespace","metadata":{"name":"valid"}}`},
			packageName: "pkg",
			expectErr:   true,
			errContains: "invalid template apiVersion and kind",
		},
		{
			name:        "rejects invalid kind from template",
			annotations: map[string]string{AnnotationSuggestedNamespaceTemplate: `{"apiVersion":"v1","kind":"NotNamespace","metadata":{"name":"valid"}}`},
			packageName: "pkg",
			expectErr:   true,
			errContains: "invalid template apiVersion and kind",
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
		name        string
		nsName      string
		labels      map[string]string
		annotations map[string]string
		want        *corev1.Namespace
	}{
		{
			name:        "with labels and annotations",
			nsName:      "my-ns",
			labels:      map[string]string{"pod-security.kubernetes.io/enforce": "restricted"},
			annotations: map[string]string{"some.io/annotation": "value"},
			want: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{Kind: "Namespace", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:        "my-ns",
					Labels:      map[string]string{"pod-security.kubernetes.io/enforce": "restricted"},
					Annotations: map[string]string{"some.io/annotation": "value"},
				},
			},
		},
		{
			name:   "no labels or annotations",
			nsName: "my-ns",
			want: &corev1.Namespace{
				TypeMeta:   metav1.TypeMeta{Kind: "Namespace", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "my-ns"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildNamespaceObject(tt.nsName, tt.labels, tt.annotations))
		})
	}
}

func TestDefaultInstallNamespace(t *testing.T) {
	tests := []struct {
		name        string
		packageName string
		want        string // exact expected name; empty means only assert validity
		// wantPrefix asserts the stable prefix of a hashed name, for package names that
		// sanitization had to alter and whose hash suffix cannot be written out here.
		wantPrefix string
	}{
		{
			name:        "valid short name keeps <package>-system",
			packageName: "argocd-operator",
			want:        "argocd-operator-system",
		},
		{
			name:        "dotted package name is normalized and hashed",
			packageName: "my.operator",
			wantPrefix:  "my-operator-",
		},
		{
			name:        "uppercase and underscores are normalized and hashed",
			packageName: "My_Operator",
			wantPrefix:  "my-operator-",
		},
		{
			name:        "overlong package name is truncated to a valid label",
			packageName: strings.Repeat("a", 80),
			// no exact expectation; validated below
		},
		{
			name:        "package name with no valid characters still yields a valid namespace",
			packageName: "...",
			// no exact expectation; validated below
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultInstallNamespace(tt.packageName)

			// The default must always be a valid, length-bounded namespace name.
			require.NoError(t, validateNamespaceName(got))

			// It must be deterministic.
			require.Equal(t, got, defaultInstallNamespace(tt.packageName))

			if tt.want != "" {
				require.Equal(t, tt.want, got)
			}
			if tt.wantPrefix != "" {
				require.True(t, strings.HasPrefix(got, tt.wantPrefix), "got %q, want prefix %q", got, tt.wantPrefix)
				require.NotEqual(t, tt.wantPrefix+"system", got,
					"a normalized name must not take the plain <package>-system form, which belongs to the package spelled that way")
			}
		})
	}

	t.Run("distinct overlong names that share a prefix do not collide", func(t *testing.T) {
		a := defaultInstallNamespace(strings.Repeat("a", 70) + "-one")
		b := defaultInstallNamespace(strings.Repeat("a", 70) + "-two")
		require.NoError(t, validateNamespaceName(a))
		require.NoError(t, validateNamespaceName(b))
		require.NotEqual(t, a, b)
	})

	t.Run("names that normalize to the same label do not collide", func(t *testing.T) {
		// "foo.bar" sanitizes to "foo-bar", which is itself a valid package name. Both are
		// installable at once, so they must not claim the same namespace.
		dotted := defaultInstallNamespace("foo.bar")
		hyphenated := defaultInstallNamespace("foo-bar")
		require.NoError(t, validateNamespaceName(dotted))
		require.NoError(t, validateNamespaceName(hyphenated))
		require.NotEqual(t, dotted, hyphenated)
		require.Equal(t, "foo-bar-system", hyphenated, "an already-valid package name keeps the plain form")
	})
}
