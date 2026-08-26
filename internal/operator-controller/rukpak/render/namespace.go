package render

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	hashutil "github.com/operator-framework/operator-controller/internal/shared/util/hash"
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
		// The auto-derived default must always be a valid namespace, even for package names
		// with disallowed characters (e.g. dots) or names that are too long.
		name = defaultInstallNamespace(rv1.PackageName)
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

const maxNamespaceNameLength = 63

func validateNamespaceName(name string) error {
	if name == "" {
		return fmt.Errorf("resolved namespace name is empty")
	}
	if len(name) > maxNamespaceNameLength {
		return fmt.Errorf("resolved namespace name %q exceeds %d characters", name, maxNamespaceNameLength)
	}
	if !dns1123LabelRegexp.MatchString(name) {
		return fmt.Errorf("resolved namespace name %q is not a valid DNS1123 label", name)
	}
	return nil
}

// defaultInstallNamespace derives a deterministic, DNS1123-label-valid namespace name for a
// package when the bundle does not suggest one. It normalizes disallowed characters (e.g. dots)
// and enforces the namespace length limit. When the normalized name must be truncated, a short
// hash of the original package name is appended to preserve deterministic collision resistance.
func defaultInstallNamespace(packageName string) string {
	const suffix = "system"

	base := sanitizeDNS1123Label(packageName)

	// Fast path: an already-valid, short base keeps the historical "<package>-system" name.
	if base != "" && len(base)+1+len(suffix) <= maxNamespaceNameLength {
		return base + "-" + suffix
	}

	// Otherwise keep the name deterministic and collision-resistant: append a short hash of the
	// original package name and truncate the base to fit within the length limit.
	hash := hashutil.DeepHashObject(packageName)[:8]
	maxBase := maxNamespaceNameLength - len(suffix) - len(hash) - 2 // account for two '-' separators
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	base = strings.Trim(base, "-")
	if base == "" {
		return hash + "-" + suffix
	}
	return base + "-" + hash + "-" + suffix
}

// sanitizeDNS1123Label lowercases s, replaces each run of disallowed characters with a single
// hyphen, and trims leading/trailing hyphens so the result is a valid DNS1123 label (or empty).
func sanitizeDNS1123Label(s string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		case !lastHyphen:
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// BuildNamespaceObject returns the Namespace object to include in the rendered set,
// seeding labels and annotations from the optional template. Empty spec/status are
// stripped to avoid apply drift.
func BuildNamespaceObject(name string, template *corev1.Namespace) (client.Object, error) {
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
