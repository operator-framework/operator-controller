package applier

import (
	"encoding/json"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

var dns1123LabelRegexp = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

const (
	AnnotationSuggestedNamespaceTemplate = "operatorframework.io/suggested-namespace-template"
	AnnotationSuggestedNamespace         = "operators.operatorframework.io/suggested-namespace"
)

func ParseNamespaceTemplate(csvAnnotations map[string]string) (*corev1.Namespace, error) {
	if csvAnnotations == nil {
		return nil, nil
	}

	templateJSON, exists := csvAnnotations[AnnotationSuggestedNamespaceTemplate]
	if !exists || templateJSON == "" {
		return nil, nil
	}

	var ns corev1.Namespace
	if err := json.Unmarshal([]byte(templateJSON), &ns); err != nil {
		return nil, fmt.Errorf("failed to parse namespace template: %w", err)
	}

	return &ns, nil
}

// ResolveNamespaceName resolves the namespace name from CSV annotations using the
// fallback chain: suggested-namespace-template name → suggested-namespace → <packageName>-system.
// Returns the resolved name, the parsed template (if any), and an error.
func ResolveNamespaceName(csvAnnotations map[string]string, packageName string) (string, *corev1.Namespace, error) {
	// Try suggested-namespace-template first
	template, err := ParseNamespaceTemplate(csvAnnotations)
	if err != nil {
		return "", nil, err
	}

	var name string
	if template != nil && template.Name != "" {
		name = template.Name
	} else if csvAnnotations != nil {
		if n, ok := csvAnnotations[AnnotationSuggestedNamespace]; ok && n != "" {
			name = n
		}
	}

	if name == "" {
		name = fmt.Sprintf("%s-system", packageName)
	}

	if err := validateNamespaceName(name); err != nil {
		return "", nil, err
	}

	return name, template, nil
}

func validateNamespaceName(name string) error {
	if name == "" {
		return fmt.Errorf("resolved namespace name is empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("resolved namespace name %q exceeds 63 characters", name)
	}
	if !dns1123LabelRegexp.MatchString(name) {
		return fmt.Errorf("resolved namespace name %q is not a valid DNS1123 label", name)
	}
	return nil
}

func BuildNamespaceObject(name string, template *corev1.Namespace) (unstructured.Unstructured, error) {
	ns := corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	if template != nil {
		if len(template.Labels) > 0 {
			ns.Labels = template.Labels
		}
		if len(template.Annotations) > 0 {
			ns.Annotations = template.Annotations
		}
	}

	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&ns)
	if err != nil {
		return unstructured.Unstructured{}, fmt.Errorf("failed to convert namespace to unstructured: %w", err)
	}

	// Remove empty spec/status to avoid apply drift, consistent with how
	// other bundle objects are sanitized before storage in a ClusterObjectSet.
	delete(unstructuredObj, "status")
	delete(unstructuredObj, "spec")

	return unstructured.Unstructured{Object: unstructuredObj}, nil
}
