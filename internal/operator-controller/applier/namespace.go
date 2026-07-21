package applier

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

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

	if template != nil {
		if template.Name != "" {
			return template.Name, template, nil
		}
		// Template exists but has no name — fall through to next option,
		// but keep the template for labels/annotations
	}

	// Try suggested-namespace annotation
	if csvAnnotations != nil {
		if name, ok := csvAnnotations[AnnotationSuggestedNamespace]; ok && name != "" {
			return name, template, nil
		}
	}

	// Fallback: <packageName>-system
	return fmt.Sprintf("%s-system", packageName), template, nil
}

func BuildNamespaceObject(name string, template *corev1.Namespace) unstructured.Unstructured {
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
		panic(fmt.Sprintf("failed to convert namespace to unstructured: %v", err))
	}

	return unstructured.Unstructured{Object: unstructuredObj}
}
