package resolve

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	bsemver "github.com/blang/semver/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/compare"
	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/filter"
	filterutil "github.com/operator-framework/operator-controller/internal/shared/util/filter"
	slicesutil "github.com/operator-framework/operator-controller/internal/shared/util/slices"
)

type ValidationFunc func(*declcfg.Bundle) error

type CatalogResolver struct {
	ListCatalogsFunc func(context.Context, ...client.ListOption) ([]ocv1.ClusterCatalog, error)
	GetPackageFunc   func(context.Context, *ocv1.ClusterCatalog, string) (*declcfg.DeclarativeConfig, error)
	Validations      []ValidationFunc
}

// Resolve returns a Bundle from a catalog that needs to get installed on the cluster.
func (r *CatalogResolver) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, *declcfg.Deprecation, error) {
	l := log.FromContext(ctx)
	packageName := ext.Spec.Source.Catalog.PackageName
	versionRange := ext.Spec.Source.Catalog.Version
	channels := ext.Spec.Source.Catalog.Channels

	// Unless overridden, select all catalogs.
	var selector = labels.Everything()
	var err error
	if ext.Spec.Source.Catalog != nil {
		selector, err = metav1.LabelSelectorAsSelector(ext.Spec.Source.Catalog.Selector)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("desired catalog selector is invalid: %w", err)
		}
		// A nothing (empty) selector selects everything
		if selector == labels.Nothing() {
			selector = labels.Everything()
		}
	}

	var versionRangeConstraints bsemver.Range
	if versionRange != "" {
		versionRangeConstraints, err = compare.NewVersionRange(versionRange)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("desired version range %q is invalid: %w", versionRange, err)
		}
	}

	cat, packageFBC, err := r.selectPackage(ctx, packageName, selector)
	if err != nil {
		return nil, nil, nil, err
	}
	if cat == nil {
		return nil, nil, nil, fmt.Errorf("package %q not found in any eligible catalog", packageName)
	}

	// A non-nil deprecation means that one catalog's package data was read,
	// even when it contains no deprecation entries or bundle resolution fails.
	deprecation := &declcfg.Deprecation{Schema: declcfg.SchemaDeprecation, Package: packageName}
	if len(packageFBC.Deprecations) > 0 {
		deprecation = &packageFBC.Deprecations[0]
	}
	failed := func(err error) (*declcfg.Bundle, *declcfg.VersionRelease, *declcfg.Deprecation, error) {
		l.Info("resolution failed", "catalog", cat.Name, "error", err)
		return nil, nil, deprecation, fmt.Errorf("resolving package %q from catalog %q: %w", packageName, cat.Name, err)
	}

	var predicates []filterutil.Predicate[declcfg.Bundle]
	if len(channels) > 0 {
		channelSet := sets.New(channels...)
		filteredChannels := slices.DeleteFunc(packageFBC.Channels, func(c declcfg.Channel) bool {
			return !channelSet.Has(c.Name)
		})
		predicates = append(predicates, filter.InAnyChannel(filteredChannels...))
	}
	if versionRangeConstraints != nil {
		predicates = append(predicates, filter.InSemverRange(versionRangeConstraints))
	}
	if ext.Spec.Source.Catalog.UpgradeConstraintPolicy != ocv1.UpgradeConstraintPolicySelfCertified && installedBundle != nil {
		successorPredicate, err := filter.SuccessorsOf(*installedBundle, packageFBC.Channels...)
		if err != nil {
			return failed(fmt.Errorf("error finding upgrade edges: %w", err))
		}
		predicates = append(predicates, successorPredicate)
	}

	// Apply bundle filters only after selecting a catalog for the package.
	packageFBC.Bundles = filterutil.InPlace(packageFBC.Bundles, filterutil.And(predicates...))
	if len(packageFBC.Bundles) == 0 {
		return failed(resolutionError{PackageName: packageName, Version: versionRange, Channels: channels, InstalledBundle: installedBundle})
	}

	// Within the selected catalog, prefer non-deprecated bundles, then the highest version and release.
	byDeprecation := compare.ByDeprecationFunc(*deprecation)
	slices.SortStableFunc(packageFBC.Bundles, func(a, b declcfg.Bundle) int {
		if lessDep := byDeprecation(a, b); lessDep != 0 {
			return lessDep
		}
		return compare.ByVersionAndRelease(a, b)
	})
	resolvedBundle := &packageFBC.Bundles[0]
	resolvedBundleVersion, err := bundleutil.GetVersionAndRelease(*resolvedBundle)
	if err != nil {
		return failed(fmt.Errorf("error getting resolved bundle version for bundle %q: %w", resolvedBundle.Name, err))
	}

	// Run validations against the resolved bundle to ensure only valid resolved bundles are being returned
	// Open Question: Should we grab the first valid bundle earlier?
	//        Answer: No, that would be a hidden resolution input, which we should avoid at all costs; the query can be
	//                constrained in order to eliminate the invalid bundle from the resolution.
	for _, validation := range r.Validations {
		if err := validation(resolvedBundle); err != nil {
			return failed(fmt.Errorf("validating bundle %q: %w", resolvedBundle.Name, err))
		}
	}

	l.V(4).Info("resolution succeeded", "catalog", cat.Name, "bundle", resolvedBundle.Name)
	return resolvedBundle, resolvedBundleVersion, deprecation, nil
}

type resolutionError struct {
	PackageName     string
	Version         string
	Channels        []string
	InstalledBundle *ocv1.BundleMetadata
}

func (rei resolutionError) Error() string {
	var sb strings.Builder
	if rei.InstalledBundle != nil {
		sb.WriteString(fmt.Sprintf("error upgrading from currently installed version %q: ", rei.InstalledBundle.Version))
	}

	sb.WriteString(fmt.Sprintf("no bundles found for package %q ", rei.PackageName))

	if rei.Version != "" {
		sb.WriteString(fmt.Sprintf("matching version %q ", rei.Version))
	}

	if len(rei.Channels) > 0 {
		sb.WriteString(fmt.Sprintf("in channels %v ", rei.Channels))
	}

	return strings.TrimSpace(sb.String())
}

func (r *CatalogResolver) selectPackage(ctx context.Context, packageName string, selector labels.Selector) (*ocv1.ClusterCatalog, *declcfg.DeclarativeConfig, error) {
	l := log.FromContext(ctx)
	catalogs, err := r.ListCatalogsFunc(ctx, client.MatchingLabelsSelector{Selector: selector})
	if err != nil {
		return nil, nil, fmt.Errorf("error listing catalogs: %w", err)
	}
	// Exclude unavailable catalogs before sorting and grouping by priority.
	catalogs = slices.DeleteFunc(catalogs, func(c ocv1.ClusterCatalog) bool {
		if c.Spec.AvailabilityMode == ocv1.AvailabilityModeUnavailable {
			l.Info("excluding ClusterCatalog from resolution process since it is disabled", "catalog", c.Name)
			return true
		}
		return false
	})
	groups := groupCatalogsByPriority(catalogs)
	l.Info("using ClusterCatalogs for resolution", "catalogs", slicesutil.Map(catalogs, func(c ocv1.ClusterCatalog) string { return c.Name }))

	for _, group := range groups {
		var selected *ocv1.ClusterCatalog
		var packageFBC *declcfg.DeclarativeConfig
		var foundIn []string
		for i := range group {
			cat := &group[i]
			fbc, err := r.GetPackageFunc(ctx, cat, packageName)
			if err != nil {
				return nil, nil, fmt.Errorf("error getting package %q from catalog %q: %w", packageName, cat.Name, err)
			}
			if isFBCEmpty(fbc) {
				continue
			}
			selected, packageFBC = cat, fbc
			foundIn = append(foundIn, cat.Name)
		}
		if len(foundIn) > 1 {
			return nil, nil, fmt.Errorf("package %q found in multiple catalogs with the same priority %d: %v", packageName, group[0].Spec.Priority, foundIn)
		}
		if selected != nil {
			return selected, packageFBC, nil
		}
	}
	return nil, nil, nil
}

// groupCatalogsByPriority sorts catalogs by descending priority and ascending name,
// then returns priority groups that share the input slice's storage.
func groupCatalogsByPriority(catalogs []ocv1.ClusterCatalog) [][]ocv1.ClusterCatalog {
	slices.SortFunc(catalogs, func(a, b ocv1.ClusterCatalog) int {
		if priority := cmp.Compare(b.Spec.Priority, a.Spec.Priority); priority != 0 {
			return priority
		}
		return cmp.Compare(a.Name, b.Name)
	})
	groups := make([][]ocv1.ClusterCatalog, 0, len(catalogs))
	for len(catalogs) > 0 {
		end := 1
		for end < len(catalogs) && catalogs[end].Spec.Priority == catalogs[0].Spec.Priority {
			end++
		}
		groups = append(groups, catalogs[:end])
		catalogs = catalogs[end:]
	}
	return groups
}

func isFBCEmpty(fbc *declcfg.DeclarativeConfig) bool {
	if fbc == nil {
		return true
	}
	return len(fbc.Packages) == 0 && len(fbc.Channels) == 0 && len(fbc.Bundles) == 0 && len(fbc.Deprecations) == 0 && len(fbc.Others) == 0
}
