package render

import (
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	hashutil "github.com/operator-framework/operator-controller/internal/shared/util/hash"
)

const (
	// AnnotationSuggestedNamespaceTemplate is a CSV annotation carrying a YAML or JSON
	// Namespace template. When present, it is authoritative and the full object is emitted.
	AnnotationSuggestedNamespaceTemplate = "operatorframework.io/suggested-namespace-template"
	// AnnotationSuggestedNamespace is a CSV annotation carrying the preferred
	// namespace name for the operator.
	AnnotationSuggestedNamespace = "operatorframework.io/suggested-namespace"
)

var dns1123LabelRegexp = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// resolveSystemManagedNamespace derives the namespace OLM should
// create and manage for a bundle, using the precedence:
//
//	suggested-namespace-template, suggested-namespace, normalized <packageName>-system
//
// Present annotations are authoritative, even if empty or invalid. It returns the
// resolved name and complete Namespace object, with core/v1 Namespace type metadata.
func resolveSystemManagedNamespace(rv1 *bundle.RegistryV1) (string, *corev1.Namespace, error) {
	csvAnnotations := rv1.CSV.GetAnnotations()

	template, err := parseNamespaceTemplate(csvAnnotations)
	if err != nil {
		return "", nil, err
	}

	// Only if there was no error and a nil template should we fall back to check for the suggested-namespace annotation
	if template == nil {
		name, exists := csvAnnotations[AnnotationSuggestedNamespace]
		if !exists {
			// If the suggested-namespace annotation ALSO does not exist, fallback again to a default namespace
			// with a name derived from the package name of the bundle
			name = defaultInstallNamespace(rv1.PackageName)
		}
		template = buildNamespaceObject(name, nil, nil)
	}

	// Check that the template included valid type metadata and a valid name.
	if template.APIVersion != "v1" || template.Kind != "Namespace" {
		return "", nil, fmt.Errorf(`invalid template apiVersion and kind %s/%s, expected v1/Namespace`, template.APIVersion, template.Kind)
	}
	if err := validateNamespaceName(template.Name); err != nil {
		return "", nil, fmt.Errorf("invalid install namespace name %q: %w", template.Name, err)
	}
	return template.Name, template, nil
}

func parseNamespaceTemplate(csvAnnotations map[string]string) (*corev1.Namespace, error) {
	templateJSON, exists := csvAnnotations[AnnotationSuggestedNamespaceTemplate]
	if !exists {
		return nil, nil
	}

	var ns corev1.Namespace
	if err := yaml.Unmarshal([]byte(templateJSON), &ns); err != nil {
		return nil, fmt.Errorf("failed to parse CSV namespace template annotation %q: %w", AnnotationSuggestedNamespaceTemplate, err)
	}

	return &ns, nil
}

const maxNamespaceNameLength = 63

func validateNamespaceName(name string) error {
	if name == "" {
		return fmt.Errorf("namespace name is empty")
	}
	if len(name) > maxNamespaceNameLength {
		return fmt.Errorf("namespace name %q exceeds %d characters", name, maxNamespaceNameLength)
	}
	if !dns1123LabelRegexp.MatchString(name) {
		return fmt.Errorf("namespace name %q is not a valid DNS1123 label", name)
	}
	return nil
}

// defaultInstallNamespace derives a deterministic, DNS1123-label-valid namespace name for a
// package when the bundle does not suggest one. It normalizes disallowed characters (e.g. dots)
// and enforces the namespace length limit. Whenever the package name has to be altered to fit —
// by normalization or truncation — a short hash of the original name is appended, so packages
// that would otherwise reduce to the same label keep distinct namespaces.
func defaultInstallNamespace(packageName string) string {
	const (
		suffix     = "system"
		hashLength = 8
		// maxBase is the room left for the sanitized package name in "<base>-<hash>-system";
		// it is a compile-time constant (47), so the truncation below can never be negative.
		maxBase = maxNamespaceNameLength - len(suffix) - hashLength - 2
	)

	base := sanitizeDNS1123Label(packageName)

	// Fast path: a package name that is already a valid, short label keeps the historical
	// "<packageName>-system" name. Names that sanitization had to alter take the hashed path
	// instead, so packages that normalize to the same label (e.g. "foo.bar" and "foo-bar")
	// do not both claim one namespace.
	if base != "" && base == packageName && len(base)+1+len(suffix) <= maxNamespaceNameLength {
		return base + "-" + suffix
	}

	// Otherwise keep the name deterministic and collision-resistant: append a short hash of the
	// original package name and truncate the base to fit within the length limit.
	hash := hashutil.DeepHashObject(packageName)[:hashLength]
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

// buildNamespaceObject returns the Namespace object to include in the rendered set,
// seeding the given labels and annotations.
func buildNamespaceObject(name string, labels, annotations map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
		},
	}
}
