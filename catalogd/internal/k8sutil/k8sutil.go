package k8sutil

import (
	"regexp"

	"k8s.io/apimachinery/pkg/util/validation"
)

var invalidNameChars = regexp.MustCompile(`[^\.\-a-zA-Z0-9]`)

// MetadataName replaces all invalid DNS characters with a dash. If the result
// is not a valid DNS subdomain, returns `result, false`. Otherwise, returns the
// `result, true`.
func MetadataName(name string) (string, bool) {
	result := invalidNameChars.ReplaceAllString(name, "-")
	return result, validation.IsDNS1123Subdomain(result) == nil
}
