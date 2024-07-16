package resolve

import (
	"context"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

type Resolver interface {
	Resolve(ctx context.Context, ext *ocv1alpha1.ClusterExtension, installedBundle *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error)
}

type Func func(ctx context.Context, ext *ocv1alpha1.ClusterExtension, installedBundle *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error)

func (f Func) Resolve(ctx context.Context, ext *ocv1alpha1.ClusterExtension, installedBundle *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
	return f(ctx, ext, installedBundle)
}
