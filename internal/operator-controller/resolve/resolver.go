package resolve

import (
	"context"

	"github.com/operator-framework/operator-registry/v2/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// Resolver selects a bundle from a source. Callers own engagement policy:
// Resolve is called only when the source's preconditions hold (for example,
// no rollout is in progress), and recovery from returned errors happens in
// the caller. Implementations must not build their own fallbacks.
type Resolver interface {
	Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, error)
}

type Func func(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, error)

func (f Func) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, error) {
	return f(ctx, ext, installedBundle)
}

// PackageDeprecationSource is an optional capability: deprecation evidence
// for the extension's desired package. Each source extracts the values it
// needs from the extension itself, so shared reconciliation never has to
// interpret source-specific fields.
//
// A source that cannot provide deprecation data simply does not implement
// these interfaces: consumers must use type assertions and report the
// absence of the capability exactly like an evaluation that found no
// deprecations. The interfaces carry evidence only; consumers must never use
// them to decide control flow such as source selection, fallback, or
// maintenance.
type PackageDeprecationSource interface {
	GetPackageDeprecation(ctx context.Context, ext *ocv1.ClusterExtension) (*declcfg.Deprecation, error)
}

// ChannelDeprecationSource is an optional capability: deprecation evidence
// for the extension's desired channels.
type ChannelDeprecationSource interface {
	GetChannelDeprecations(ctx context.Context, ext *ocv1.ClusterExtension) (*declcfg.Deprecation, error)
}

// BundleDeprecationSource is an optional capability: deprecation evidence
// for the given bundles (for example, the installed bundle and any bundles
// in a rollout in progress).
type BundleDeprecationSource interface {
	GetBundleDeprecations(ctx context.Context, ext *ocv1.ClusterExtension, bundles ...ocv1.BundleMetadata) (*declcfg.Deprecation, error)
}
