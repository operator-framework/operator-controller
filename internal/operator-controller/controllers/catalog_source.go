/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/operator-framework/operator-registry/v2/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/operator-controller/resolve"
)

// CatalogSource preserves Catalog resolution and lifecycle policies.
type CatalogSource struct {
	Resolver resolve.Resolver
	Client   client.Client
}

var (
	_ Resolver                         = (*CatalogSource)(nil)
	_ resolve.PackageDeprecationSource = (*CatalogSource)(nil)
	_ resolve.ChannelDeprecationSource = (*CatalogSource)(nil)
	_ resolve.BundleDeprecationSource  = (*CatalogSource)(nil)
)

// ValidateSource rejects a missing Catalog payload before any rollout
// continuation, resolution, or catalog access.
func (s *CatalogSource) ValidateSource(ext *ocv1.ClusterExtension) error {
	if ext.Spec.Source.Catalog == nil {
		return fmt.Errorf("catalog source requires spec.source.catalog")
	}
	return nil
}

// Resolve selects a Catalog bundle for the extension.
func (s *CatalogSource) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*RevisionMetadata, error) {
	log.FromContext(ctx).V(1).Info("resolving bundle")
	bundle, version, err := s.Resolver.Resolve(ctx, ext, installedBundle)
	if err != nil {
		return nil, err
	}
	return &RevisionMetadata{
		Package: bundle.Package,
		Image:   bundle.Image,
		// Preserve MetadataFor's feature-gated version/release normalization.
		BundleMetadata: bundleutil.MetadataFor(bundle.Name, *version),
	}, nil
}

// RecoverFromResolutionError owns recovery when Catalog resolution fails.
//
// Decision logic (evaluated in order):
//  1. No installed bundle → Retry (cannot proceed without any bundle)
//  2. Version change requested → Retry (cannot upgrade without catalog)
//  3. Cannot check catalog existence → Retry (API error, cannot safely decide)
//  4. Catalogs exist → Retry (transient error, catalog may be updating)
//  5. Catalogs deleted → Fallback to installed bundle (maintain current state)
//
// When falling back (case 5), we return the installed bundle and no error,
// allowing the Apply step to run and maintain resources using the existing
// installation. The controller watches ClusterCatalog resources, so
// reconciliation will automatically resume when catalogs return, enabling
// upgrades.
func (s *CatalogSource) RecoverFromResolutionError(ctx context.Context, ext *ocv1.ClusterExtension, revisions *RevisionStates, resolveErr error) (*RevisionMetadata, error) {
	l := log.FromContext(ctx)

	// No installed bundle and resolution failed - cannot proceed
	if revisions.Installed == nil {
		msg := fmt.Sprintf("failed to resolve bundle: %v", resolveErr)
		setStatusProgressing(ext, resolveErr)
		setInstalledStatusFromRevisionStates(ext, revisions)
		ensureFailureConditionsWithReason(ext, ocv1.ReasonRetrying, msg)
		return nil, resolveErr
	}

	// Check if the spec is requesting a specific version that differs from installed
	specVersion := ""
	if ext.Spec.Source.Catalog != nil {
		specVersion = ext.Spec.Source.Catalog.Version
	}
	installedVersion := revisions.Installed.Version

	// If spec requests a different version, we cannot fall back - must fail and retry
	if specVersion != "" && specVersion != installedVersion {
		msg := fmt.Sprintf("unable to upgrade to version %s: %v (currently installed: %s)", specVersion, resolveErr, installedVersion)
		l.Error(resolveErr, "resolution failed and spec requests version change - cannot fall back",
			"requestedVersion", specVersion,
			"installedVersion", installedVersion)
		setStatusProgressing(ext, resolveErr)
		setInstalledStatusFromRevisionStates(ext, revisions)
		ensureFailureConditionsWithReason(ext, ocv1.ReasonRetrying, msg)
		return nil, resolveErr
	}

	// No version change requested - check if ClusterCatalogs exist
	// Only fall back if ClusterCatalogs have been deleted
	catalogsExist, catalogCheckErr := CheckCatalogsExist(ctx, s.Client, ext)
	if catalogCheckErr != nil {
		msg := fmt.Sprintf("failed to resolve bundle: %v", resolveErr)
		var catalogName string
		if ext.Spec.Source.Catalog != nil {
			catalogName = getCatalogNameFromSelector(ext.Spec.Source.Catalog.Selector)
		}
		l.Error(catalogCheckErr, "error checking if ClusterCatalogs exist, will retry resolution",
			"resolutionError", resolveErr,
			"packageName", getPackageName(ext),
			"catalogName", catalogName)
		setStatusProgressing(ext, resolveErr)
		setInstalledStatusFromRevisionStates(ext, revisions)
		ensureFailureConditionsWithReason(ext, ocv1.ReasonRetrying, msg)
		return nil, resolveErr
	}

	if catalogsExist {
		// ClusterCatalogs exist but resolution failed - likely a transient issue (ClusterCatalog updating, cache stale, etc.)
		// Retry resolution instead of falling back
		msg := fmt.Sprintf("failed to resolve bundle, retrying: %v", resolveErr)
		var catalogName string
		if ext.Spec.Source.Catalog != nil {
			catalogName = getCatalogNameFromSelector(ext.Spec.Source.Catalog.Selector)
		}
		l.Error(resolveErr, "resolution failed but matching ClusterCatalogs exist - retrying instead of falling back",
			"packageName", getPackageName(ext),
			"catalogName", catalogName)
		setStatusProgressing(ext, resolveErr)
		setInstalledStatusFromRevisionStates(ext, revisions)
		ensureFailureConditionsWithReason(ext, ocv1.ReasonRetrying, msg)
		return nil, resolveErr
	}

	// ClusterCatalogs don't exist (deleted) - fall back to installed bundle to maintain current state.
	// The controller watches ClusterCatalog resources, so when ClusterCatalogs become available again,
	// a reconcile will be triggered automatically, allowing the extension to upgrade.
	var catalogName string
	if ext.Spec.Source.Catalog != nil {
		catalogName = getCatalogNameFromSelector(ext.Spec.Source.Catalog.Selector)
	}
	l.Info("matching ClusterCatalogs unavailable or deleted - falling back to installed bundle to maintain workload",
		"resolutionError", resolveErr.Error(),
		"packageName", getPackageName(ext),
		"catalogName", catalogName,
		"installedBundle", revisions.Installed.Name,
		"installedVersion", revisions.Installed.Version)
	// Set installed status based on current revision states (needed before Apply runs)
	setInstalledStatusFromRevisionStates(ext, revisions)
	// Return the installed bundle so the Apply step runs and maintains resources.
	// Apply will set Progressing=Succeeded when it completes successfully.
	return maintainInstalled(revisions), nil
}

// GetPackageDeprecation returns catalog deprecation evidence for the desired
// package. CatalogSource requires a resolver that provides deprecations;
// production wiring always uses *resolve.CatalogResolver.
func (s *CatalogSource) GetPackageDeprecation(ctx context.Context, ext *ocv1.ClusterExtension) (*declcfg.Deprecation, error) {
	deprecations, ok := s.Resolver.(resolve.PackageDeprecationSource)
	if !ok {
		return nil, fmt.Errorf("catalog source requires a resolver that provides package deprecations (got %T)", s.Resolver)
	}
	return deprecations.GetPackageDeprecation(ctx, ext)
}

// GetChannelDeprecations returns catalog deprecation evidence for the desired
// channels; channel-scoped entries are filtered when the evidence is
// projected onto conditions.
func (s *CatalogSource) GetChannelDeprecations(ctx context.Context, ext *ocv1.ClusterExtension) (*declcfg.Deprecation, error) {
	deprecations, ok := s.Resolver.(resolve.ChannelDeprecationSource)
	if !ok {
		return nil, fmt.Errorf("catalog source requires a resolver that provides channel deprecations (got %T)", s.Resolver)
	}
	return deprecations.GetChannelDeprecations(ctx, ext)
}

// GetBundleDeprecations returns catalog deprecation evidence for the given
// bundles; bundle-scoped entries are filtered when the evidence is projected
// onto conditions.
func (s *CatalogSource) GetBundleDeprecations(ctx context.Context, ext *ocv1.ClusterExtension, bundles ...ocv1.BundleMetadata) (*declcfg.Deprecation, error) {
	deprecations, ok := s.Resolver.(resolve.BundleDeprecationSource)
	if !ok {
		return nil, fmt.Errorf("catalog source requires a resolver that provides bundle deprecations (got %T)", s.Resolver)
	}
	return deprecations.GetBundleDeprecations(ctx, ext, bundles...)
}

// ReconcileOnCatalogChange preserves the existing fan-out to Catalog extensions.
// Catalog-to-extension provenance and selector-specific filtering remain unchanged.
func (s *CatalogSource) ReconcileOnCatalogChange(*ocv1.ClusterExtension, *ocv1.ClusterCatalog) bool {
	return true
}

// CanMaintainWithoutContent preserves the original name/version comparison.
func (s *CatalogSource) CanMaintainWithoutContent(_ *ocv1.ClusterExtension, revisions *RevisionStates, resolved *RevisionMetadata) bool {
	return revisions != nil && revisions.Installed != nil && resolved != nil &&
		resolved.Name == revisions.Installed.Name &&
		resolved.Version == revisions.Installed.Version
}

// getCatalogNameFromSelector extracts the catalog name from the selector if available.
// Returns empty string if selector is nil or doesn't contain the metadata.name label.
func getCatalogNameFromSelector(selector *metav1.LabelSelector) string {
	if selector == nil || selector.MatchLabels == nil {
		return ""
	}
	return selector.MatchLabels["olm.operatorframework.io/metadata.name"]
}

// getPackageName safely extracts the package name from the extension spec.
// Returns empty string if Catalog source is nil.
func getPackageName(ext *ocv1.ClusterExtension) string {
	if ext.Spec.Source.Catalog == nil {
		return ""
	}
	return ext.Spec.Source.Catalog.PackageName
}

// CheckCatalogsExist checks if any ClusterCatalogs matching the extension's selector exist.
// Returns true if at least one matching ClusterCatalog exists, false if none exist.
// Treats "CRD doesn't exist" errors as "no ClusterCatalogs exist" (returns false, nil).
// Returns an error only if the check itself fails unexpectedly.
func CheckCatalogsExist(ctx context.Context, c client.Client, ext *ocv1.ClusterExtension) (bool, error) {
	if ext.Spec.Source.SourceType != ocv1.SourceTypeCatalog || ext.Spec.Source.Catalog == nil {
		return false, fmt.Errorf("catalog existence checks require a Catalog source with spec.source.catalog")
	}
	var catalogList *ocv1.ClusterCatalogList
	var listErr error

	if ext.Spec.Source.Catalog.Selector == nil {
		// No selector means all ClusterCatalogs match - check if any ClusterCatalogs exist at all
		catalogList = &ocv1.ClusterCatalogList{}
		listErr = c.List(ctx, catalogList, client.Limit(1))
	} else {
		// Convert label selector to k8slabels.Selector
		// Note: An empty LabelSelector matches everything by default
		selector, err := metav1.LabelSelectorAsSelector(ext.Spec.Source.Catalog.Selector)
		if err != nil {
			return false, fmt.Errorf("invalid catalog selector: %w", err)
		}

		// List ClusterCatalogs matching the selector (limit to 1 since we only care if any exist)
		catalogList = &ocv1.ClusterCatalogList{}
		listErr = c.List(ctx, catalogList, client.MatchingLabelsSelector{Selector: selector}, client.Limit(1))
	}

	if listErr != nil {
		// Check if the error is because the ClusterCatalog CRD doesn't exist
		// This can happen if catalogd is not installed, which means no ClusterCatalogs exist
		if apimeta.IsNoMatchError(listErr) {
			return false, nil
		}
		return false, fmt.Errorf("failed to list ClusterCatalogs: %w", listErr)
	}

	return len(catalogList.Items) > 0, nil
}
