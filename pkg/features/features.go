package features

import (
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

const (
	// Add new feature gates constants (strings)
	// Ex: SomeFeature featuregate.Feature = "SomeFeature"

	CatalogMetadataAPI         featuregate.Feature = "CatalogMetadataAPI"
	PackagesBundleMetadataAPIs featuregate.Feature = "PackagesBundleMetadataAPIs"
)

var catalogdFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	// Add new feature gate definitions
	// Ex: SomeFeature: {...}

	PackagesBundleMetadataAPIs: {Default: false, PreRelease: featuregate.Deprecated},

	// Marking the CatalogMetadataAPI feature gate as Deprecated in the interest of introducing
	// the HTTP Server functionality in the future and use it as a default method of serving the catalog contents.
	CatalogMetadataAPI: {Default: false, PreRelease: featuregate.Deprecated},
}

var CatalogdFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	utilruntime.Must(CatalogdFeatureGate.Add(catalogdFeatureGates))
}
