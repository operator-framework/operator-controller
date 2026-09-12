package resolve

import (
	"context"
	"fmt"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type Resolver interface {
	Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, *declcfg.Deprecation, error)
}

// ResolverBehavior describes source-specific reconciliation behavior that
// cannot be inferred from a resolved bundle alone.
type ResolverBehavior interface {
	HasCatalogData(*ocv1.ClusterExtension) bool
	ShouldFallbackOnError(*ocv1.ClusterExtension) bool
}

type Func func(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, *declcfg.Deprecation, error)

func (f Func) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, *declcfg.Deprecation, error) {
	return f(ctx, ext, installedBundle)
}

// MultiResolver dispatches bundle resolution by ClusterExtension source type.
type MultiResolver map[string]Resolver

// RegisterType associates a source type with its resolver.
func (m MultiResolver) RegisterType(sourceType string, resolver Resolver) {
	m[sourceType] = resolver
}

// Resolve dispatches to the resolver selected by the ClusterExtension source type.
func (m MultiResolver) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, *declcfg.Deprecation, error) {
	resolver, ok := m[ext.Spec.Source.SourceType]
	if !ok {
		return nil, nil, nil, fmt.Errorf("no resolver for source type %q", ext.Spec.Source.SourceType)
	}
	return resolver.Resolve(ctx, ext, installedBundle)
}

// HasCatalogData reports whether the selected source can provide catalog metadata.
func (m MultiResolver) HasCatalogData(ext *ocv1.ClusterExtension) bool {
	return ext.Spec.Source.SourceType == ocv1.SourceTypeCatalog
}

// ShouldFallbackOnError reports whether catalog fallback behavior is valid for the source.
func (m MultiResolver) ShouldFallbackOnError(ext *ocv1.ClusterExtension) bool {
	return ext.Spec.Source.SourceType == ocv1.SourceTypeCatalog
}
