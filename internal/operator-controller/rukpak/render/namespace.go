package render

import (
	"encoding/json"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
)

const (
	// AnnotationSuggestedNamespaceTemplate is a CSV annotation carrying a JSON
	// Namespace template whose metadata seeds the system-managed namespace.
	AnnotationSuggestedNamespaceTemplate = "operatorframework.io/suggested-namespace-template"
	// AnnotationSuggestedNamespace is a CSV annotation carrying the preferred
	// namespace name for the operator.
	AnnotationSuggestedNamespace = "operatorframework.io/suggested-namespace"
)

var dns1123LabelRegexp = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// resolveSystemManagedNamespace derives the name of the namespace OLM should
// create and manage for a bundle, using the precedence:
//
//	suggested-namespace-template name → suggested-namespace → <packageName>-system
//
// It returns the resolved name and the parsed template (if any) so the caller can
// seed labels/annotations (e.g. PSA) on the emitted Namespace object.
func resolveSystemManagedNamespace(rv1 *bundle.RegistryV1) (string, *corev1.Namespace, error) {
	csvAnnotations := rv1.CSV.GetAnnotations()

	template, err := parseNamespaceTemplate(csvAnnotations)
	if err != nil {
		return "", nil, err
	}

	var name string
	switch {
	case template != nil && template.Name != "":
		name = template.Name
	case csvAnnotations[AnnotationSuggestedNamespace] != "":
		name = csvAnnotations[AnnotationSuggestedNamespace]
	default:
		name = fmt.Sprintf("%s-system", rv1.PackageName)
	}

	if err := validateNamespaceName(name); err != nil {
		return "", nil, err
	}

	return name, template, nil
}

func parseNamespaceTemplate(csvAnnotations map[string]string) (*corev1.Namespace, error) {
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

// buildNamespaceObject returns the Namespace object to include in the rendered set,
// seeding labels and annotations from the optional template. Empty spec/status are
// stripped to avoid apply drift.
func buildNamespaceObject(name string, template *corev1.Namespace) (client.Object, error) {
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
		return nil, fmt.Errorf("failed to convert namespace to unstructured: %w", err)
	}
	delete(unstructuredObj, "status")
	delete(unstructuredObj, "spec")

	return &unstructured.Unstructured{Object: unstructuredObj}, nil
}
