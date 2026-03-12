package compare

import (
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

// ByVersion is a sort "less" function that orders bundles
// in inverse version order (higher versions on top).
//
// When the CompositeVersionComparison feature gate is enabled, it uses the
// Bundle.Compare method from operator-registry which properly handles
// both semantic version and release metadata (from the olm.package property).
//
// When the feature gate is disabled, it uses the legacy comparison that only
// compares semantic versions without release metadata.
func ByVersion(b1, b2 declcfg.Bundle) int {
	if features.OperatorControllerFeatureGate.Enabled(features.CompositeVersionComparison) {
		// Use the Bundle.Compare method which handles version + release
		// Bundle.Compare returns:
		//  - negative if b1 < b2
		//  - 0 if b1 == b2
		//  - positive if b1 > b2
		// We want higher versions on top, so we reverse the comparison
		return b2.Compare(&b1)
	}

	// Legacy comparison: only compare semantic versions
	v1, err1 := bundleutil.GetVersion(b1)
	v2, err2 := bundleutil.GetVersion(b2)
	if err1 != nil || err2 != nil {
		return compareErrors(err1, err2)
	}

	// Check for "greater than" because
	// we want higher versions on top
	return v2.Compare(*v1)
}

func ByDeprecationFunc(deprecation declcfg.Deprecation) func(a, b declcfg.Bundle) int {
	deprecatedBundles := sets.New[string]()
	for _, entry := range deprecation.Entries {
		if entry.Reference.Schema == declcfg.SchemaBundle {
			deprecatedBundles.Insert(entry.Reference.Name)
		}
	}
	return func(a, b declcfg.Bundle) int {
		aDeprecated := deprecatedBundles.Has(a.Name)
		bDeprecated := deprecatedBundles.Has(b.Name)
		if aDeprecated && !bDeprecated {
			return 1
		}
		if !aDeprecated && bDeprecated {
			return -1
		}
		return 0
	}
}

// compareErrors returns 0 if both errors are either nil or not nil
// -1 if err1 is nil and err2 is not nil
// +1 if err1 is not nil and err2 is nil
func compareErrors(err1 error, err2 error) int {
	if err1 != nil && err2 == nil {
		return 1
	}

	if err1 == nil && err2 != nil {
		return -1
	}
	return 0
}
