package resolve

import (
	"context"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

// PackageProvider defines an API for providing bundle and deprecation info
// from a catalog for a specific package without any filtering applied.
type PackageProvider interface {
	// GetPackage returns the raw package data (bundles, channels, deprecations)
	// for the specified package from catalogs matching the selector.
	GetPackage(ctx context.Context, packageName string, selector CatalogSelector) (*PackageData, error)
}

// CatalogSelector defines criteria for selecting catalogs
type CatalogSelector struct {
	// LabelSelector filters catalogs by labels
	LabelSelector string
}

// PackageData contains unfiltered package information from one or more catalogs
type PackageData struct {
	// CatalogPackages maps catalog name to its package data
	CatalogPackages map[string]*CatalogPackage
}

// CatalogPackage represents package data from a single catalog
type CatalogPackage struct {
	Name         string
	Priority     int32
	Bundles      []declcfg.Bundle
	Channels     []declcfg.Channel
	Deprecations []declcfg.Deprecation
}

// BundleRef references a bundle with its source catalog
type BundleRef struct {
	Bundle   *declcfg.Bundle
	Catalog  string
	Priority int32
}
