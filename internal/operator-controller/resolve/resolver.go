package resolve

import (
	"context"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type Resolver interface {
	Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error)
}

type Func func(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error)

func (f Func) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
	return f(ctx, ext, installedBundle)
}

// MultiResolver uses the CatalogResolver by default. It will use the currently internal,feature gated, and annotation-powered BundleImageRefResolver
// if it is non-nil and the necessary annotation is present
type MultiResolver struct {
	CatalogResolver CatalogResolver
	BundleResolver  *BundleImageRefResolver
}

func (m MultiResolver) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
	if m.BundleResolver != nil && ext.Annotations != nil && ext.Annotations[directBundleInstallImageAnnotation] != "" {
		return m.BundleResolver.Resolve(ctx, ext, installedBundle)
	}
	return m.CatalogResolver.Resolve(ctx, ext, installedBundle)
}
