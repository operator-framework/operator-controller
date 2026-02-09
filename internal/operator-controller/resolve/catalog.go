package resolve

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	bsemver "github.com/blang/semver/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/compare"
	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/filter"
	filterutil "github.com/operator-framework/operator-controller/internal/shared/util/filter"
	slicesutil "github.com/operator-framework/operator-controller/internal/shared/util/slices"
)

type ValidationFunc func(*declcfg.Bundle) error

type CatalogResolver struct {
	WalkCatalogsFunc func(context.Context, string, CatalogWalkFunc, ...client.ListOption) error
	Validations      []ValidationFunc
}

type foundBundle struct {
	bundle   *declcfg.Bundle
	catalog  string
	priority int32
}

// Resolve returns a Bundle from a catalog that needs to get installed on the cluster.
func (r *CatalogResolver) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
	l := log.FromContext(ctx)
	packageName := ext.Spec.Source.Catalog.PackageName
	versionRange := ext.Spec.Source.Catalog.Version
	channels := ext.Spec.Source.Catalog.Channels

	// unless overridden, default to selecting all bundles
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

	type catStat struct {
		CatalogName    string `json:"catalogName"`
		PackageFound   bool   `json:"packageFound"`
		TotalBundles   int    `json:"totalBundles"`
		MatchedBundles int    `json:"matchedBundles"`
	}

	var catStats []*catStat

	var resolvedBundles []foundBundle
	var priorDeprecation *declcfg.Deprecation

	listOptions := []client.ListOption{
		client.MatchingLabelsSelector{Selector: selector},
	}
	if err := r.WalkCatalogsFunc(ctx, packageName, func(ctx context.Context, cat *ocv1.ClusterCatalog, packageFBC *declcfg.DeclarativeConfig, err error) error {
		if err != nil {
			return fmt.Errorf("error getting package %q from catalog %q: %w", packageName, cat.Name, err)
		}

		cs := catStat{CatalogName: cat.Name}
		catStats = append(catStats, &cs)

		if isFBCEmpty(packageFBC) {
			return nil
		}

		cs.PackageFound = true
		cs.TotalBundles = len(packageFBC.Bundles)

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
				return fmt.Errorf("error finding upgrade edges: %w", err)
			}
			predicates = append(predicates, successorPredicate)
		}

		// Apply the predicates to get the candidate bundles
		packageFBC.Bundles = filterutil.InPlace(packageFBC.Bundles, filterutil.And(predicates...))
		cs.MatchedBundles = len(packageFBC.Bundles)
		if len(packageFBC.Bundles) == 0 {
			return nil
		}

		// If this package has a deprecation, we:
		//   1. Want to sort deprecated bundles to the end of the list
		//   2. Want to keep track of it so that we can return it if we end
		//      up resolving a bundle from this package.
		byDeprecation := func(a, b declcfg.Bundle) int { return 0 }
		var thisDeprecation *declcfg.Deprecation
		if len(packageFBC.Deprecations) > 0 {
			thisDeprecation = &packageFBC.Deprecations[0]
			byDeprecation = compare.ByDeprecationFunc(*thisDeprecation)
		}

		// Sort the bundles by deprecation and then by version
		slices.SortStableFunc(packageFBC.Bundles, func(a, b declcfg.Bundle) int {
			if lessDep := byDeprecation(a, b); lessDep != 0 {
				return lessDep
			}
			return compare.ByVersionAndRelease(a, b)
		})

		thisBundle := packageFBC.Bundles[0]

		if len(resolvedBundles) != 0 {
			// We've already found one or more package candidates
			currentIsDeprecated := isDeprecated(thisBundle, thisDeprecation)
			priorIsDeprecated := isDeprecated(*resolvedBundles[len(resolvedBundles)-1].bundle, priorDeprecation)
			if currentIsDeprecated && !priorIsDeprecated {
				// Skip this deprecated package and retain the non-deprecated package(s)
				return nil
			} else if !currentIsDeprecated && priorIsDeprecated {
				// Our package candidates so far were deprecated and this one is not; clear the lists
				resolvedBundles = []foundBundle{}
			}
		}
		// The current bundle shares deprecation status with prior bundles or
		// there are no prior bundles. Add it to the list.
		resolvedBundles = append(resolvedBundles, foundBundle{&thisBundle, cat.GetName(), cat.Spec.Priority})
		priorDeprecation = thisDeprecation
		return nil
	}, listOptions...); err != nil {
		return nil, nil, nil, fmt.Errorf("error walking catalogs: %w", err)
	}

	// Resolve for priority
	if len(resolvedBundles) > 1 {
		// Want highest first (reverse sort)
		sort.Slice(resolvedBundles, func(i, j int) bool { return resolvedBundles[i].priority > resolvedBundles[j].priority })
		// If the top two bundles do not have the same priority, then priority breaks the tie
		// Reduce resolvedBundles to just the first item (highest priority)
		if resolvedBundles[0].priority != resolvedBundles[1].priority {
			resolvedBundles = []foundBundle{resolvedBundles[0]}
		}
	}

	// Check for ambiguity
	if len(resolvedBundles) != 1 {
		l.Info("resolution failed", "stats", catStats)
		return nil, nil, nil, resolutionError{
			PackageName:     packageName,
			Version:         versionRange,
			Channels:        channels,
			InstalledBundle: installedBundle,
			ResolvedBundles: resolvedBundles,
		}
	}
	resolvedBundle := resolvedBundles[0].bundle
	resolvedBundleVersion, err := bundleutil.GetVersionAndRelease(*resolvedBundle)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error getting resolved bundle version for bundle %q: %w", resolvedBundle.Name, err)
	}

	// Run validations against the resolved bundle to ensure only valid resolved bundles are being returned
	// Open Question: Should we grab the first valid bundle earlier?
	//        Answer: No, that would be a hidden resolution input, which we should avoid at all costs; the query can be
	//                constrained in order to eliminate the invalid bundle from the resolution.
	for _, validation := range r.Validations {
		if err := validation(resolvedBundle); err != nil {
			return nil, nil, nil, fmt.Errorf("validating bundle %q: %w", resolvedBundle.Name, err)
		}
	}

	l.V(4).Info("resolution succeeded", "stats", catStats)
	return resolvedBundle, resolvedBundleVersion, priorDeprecation, nil
}

type resolutionError struct {
	PackageName     string
	Version         string
	Channels        []string
	InstalledBundle *ocv1.BundleMetadata
	ResolvedBundles []foundBundle
}

func (rei resolutionError) Error() string {
	var sb strings.Builder
	if rei.InstalledBundle != nil {
		sb.WriteString(fmt.Sprintf("error upgrading from currently installed version %q: ", rei.InstalledBundle.Version))
	}

	if len(rei.ResolvedBundles) > 1 {
		sb.WriteString(fmt.Sprintf("found bundles for package %q ", rei.PackageName))
	} else {
		sb.WriteString(fmt.Sprintf("no bundles found for package %q ", rei.PackageName))
	}

	if rei.Version != "" {
		sb.WriteString(fmt.Sprintf("matching version %q ", rei.Version))
	}

	if len(rei.Channels) > 0 {
		sb.WriteString(fmt.Sprintf("in channels %v ", rei.Channels))
	}

	matchedCatalogs := make([]string, 0, len(rei.ResolvedBundles))
	for _, r := range rei.ResolvedBundles {
		matchedCatalogs = append(matchedCatalogs, r.catalog)
	}
	slices.Sort(matchedCatalogs) // sort for consistent error message
	if len(matchedCatalogs) > 1 {
		sb.WriteString(fmt.Sprintf("in multiple catalogs with the same priority %v ", matchedCatalogs))
	}

	return strings.TrimSpace(sb.String())
}

func isDeprecated(bundle declcfg.Bundle, deprecation *declcfg.Deprecation) bool {
	if deprecation == nil {
		return false
	}
	for _, entry := range deprecation.Entries {
		if entry.Reference.Schema == declcfg.SchemaBundle && entry.Reference.Name == bundle.Name {
			return true
		}
	}
	return false
}

type CatalogWalkFunc func(context.Context, *ocv1.ClusterCatalog, *declcfg.DeclarativeConfig, error) error

func CatalogWalker(
	listCatalogs func(context.Context, ...client.ListOption) ([]ocv1.ClusterCatalog, error),
	getPackage func(context.Context, *ocv1.ClusterCatalog, string) (*declcfg.DeclarativeConfig, error),
) func(ctx context.Context, packageName string, f CatalogWalkFunc, catalogListOpts ...client.ListOption) error {
	return func(ctx context.Context, packageName string, f CatalogWalkFunc, catalogListOpts ...client.ListOption) error {
		l := log.FromContext(ctx)
		catalogs, err := listCatalogs(ctx, catalogListOpts...)
		if err != nil {
			return fmt.Errorf("error listing catalogs: %w", err)
		}

		// Remove disabled catalogs from consideration
		catalogs = slices.DeleteFunc(catalogs, func(c ocv1.ClusterCatalog) bool {
			if c.Spec.AvailabilityMode == ocv1.AvailabilityModeUnavailable {
				l.Info("excluding ClusterCatalog from resolution process since it is disabled", "catalog", c.Name)
				return true
			}
			return false
		})

		availableCatalogNames := slicesutil.Map(catalogs, func(c ocv1.ClusterCatalog) string { return c.Name })
		l.Info("using ClusterCatalogs for resolution", "catalogs", availableCatalogNames)

		for i := range catalogs {
			cat := &catalogs[i]

			// process enabled catalogs
			fbc, fbcErr := getPackage(ctx, cat, packageName)

			if walkErr := f(ctx, cat, fbc, fbcErr); walkErr != nil {
				return walkErr
			}
		}

		return nil
	}
}

func isFBCEmpty(fbc *declcfg.DeclarativeConfig) bool {
	if fbc == nil {
		return true
	}
	return len(fbc.Packages) == 0 && len(fbc.Channels) == 0 && len(fbc.Bundles) == 0 && len(fbc.Deprecations) == 0 && len(fbc.Others) == 0
}
