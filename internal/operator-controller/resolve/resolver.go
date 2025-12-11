package resolve

import (
	"context"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
)

type Resolver interface {
	Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error)
}

type Func func(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error)

func (f Func) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
	return f(ctx, ext, installedBundle)
}
