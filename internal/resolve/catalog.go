package resolve

import (
	"context"
	"fmt"
	"slices"

	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/bundleutil"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/compare"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
)

type ValidationFunc func(*declcfg.Bundle) error

type CatalogResolver struct {
	WalkCatalogsFunc func(context.Context, string, CatalogWalkFunc, ...client.ListOption) error
	Validations      []ValidationFunc
}

// Resolve returns a Bundle from a catalog that needs to get installed on the cluster.
func (r *CatalogResolver) Resolve(ctx context.Context, ext *ocv1alpha1.ClusterExtension, installedBundle *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
	packageName := ext.Spec.PackageName
	versionRange := ext.Spec.Version
	channelName := ext.Spec.Channel

	selector, err := metav1.LabelSelectorAsSelector(&ext.Spec.CatalogSelector)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("desired catalog selector is invalid: %w", err)
	}
	// A nothing (empty) seletor selects everything
	if selector == labels.Nothing() {
		selector = labels.Everything()
	}

	var versionRangeConstraints *mmsemver.Constraints
	if versionRange != "" {
		versionRangeConstraints, err = mmsemver.NewConstraint(versionRange)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("desired version range %q is invalid: %w", versionRange, err)
		}
	}

	var (
		resolvedBundles  []*declcfg.Bundle
		matchedCatalogs  []string
		priorDeprecation *declcfg.Deprecation
	)

	listOptions := []client.ListOption{
		client.MatchingLabelsSelector{Selector: selector},
	}
	if err := r.WalkCatalogsFunc(ctx, packageName, func(ctx context.Context, cat *catalogd.ClusterCatalog, packageFBC *declcfg.DeclarativeConfig, err error) error {
		if err != nil {
			return fmt.Errorf("error getting package %q from catalog %q: %w", packageName, cat.Name, err)
		}

		var predicates []filter.Predicate[declcfg.Bundle]
		if channelName != "" {
			channels := slices.DeleteFunc(packageFBC.Channels, func(c declcfg.Channel) bool {
				return channelName != "" && c.Name != channelName
			})
			predicates = append(predicates, filter.InAnyChannel(channels...))
		}

		if versionRangeConstraints != nil {
			predicates = append(predicates, filter.InMastermindsSemverRange(versionRangeConstraints))
		}

		if ext.Spec.UpgradeConstraintPolicy != ocv1alpha1.UpgradeConstraintPolicyIgnore && installedBundle != nil {
			successorPredicate, err := filter.SuccessorsOf(installedBundle, packageFBC.Channels...)
			if err != nil {
				return fmt.Errorf("error finding upgrade edges: %w", err)
			}
			predicates = append(predicates, successorPredicate)
		}

		// Apply the predicates to get the candidate bundles
		packageFBC.Bundles = filter.Filter(packageFBC.Bundles, filter.And(predicates...))
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
			return compare.ByVersion(a, b)
		})

		thisBundle := packageFBC.Bundles[0]

		if len(resolvedBundles) != 0 {
			// We've already found one or more package candidates
			currentIsDeprecated := isDeprecated(thisBundle, thisDeprecation)
			priorIsDeprecated := isDeprecated(*resolvedBundles[0], priorDeprecation) // Slice index doesn't matter; the whole slice is either deprecated or not
			if currentIsDeprecated && !priorIsDeprecated {
				// Skip this deprecated package and retain the non-deprecated package(s)
				return nil
			} else if !currentIsDeprecated && priorIsDeprecated {
				// Our package candidates so far were deprecated and this one is not; clear the lists
				resolvedBundles = []*declcfg.Bundle{}
				matchedCatalogs = []string{}
			}
		}
		// The current bundle shares deprecation status with prior bundles or
		// there are no prior bundles. Add it to the list.
		resolvedBundles = append(resolvedBundles, &thisBundle)
		matchedCatalogs = append(matchedCatalogs, cat.GetName())
		priorDeprecation = thisDeprecation
		return nil
	}, listOptions...); err != nil {
		return nil, nil, nil, fmt.Errorf("error walking catalogs: %w", err)
	}

	if len(resolvedBundles) != 1 {
		errPrefix := ""
		if installedBundle != nil {
			errPrefix = fmt.Sprintf("error upgrading from currently installed version %q: ", installedBundle.Version)
		}
		switch {
		case len(resolvedBundles) > 1:
			slices.Sort(matchedCatalogs) // sort for consistent error message
			return nil, nil, nil, fmt.Errorf("%smatching packages found in multiple catalogs: %v", errPrefix, matchedCatalogs)
		case versionRange != "" && channelName != "":
			return nil, nil, nil, fmt.Errorf("%sno package %q matching version %q in channel %q found", errPrefix, packageName, versionRange, channelName)
		case versionRange != "":
			return nil, nil, nil, fmt.Errorf("%sno package %q matching version %q found", errPrefix, packageName, versionRange)
		case channelName != "":
			return nil, nil, nil, fmt.Errorf("%sno package %q in channel %q found", errPrefix, packageName, channelName)
		default:
			return nil, nil, nil, fmt.Errorf("%sno package %q found", errPrefix, packageName)
		}
	}
	resolvedBundle := resolvedBundles[0]
	resolvedBundleVersion, err := bundleutil.GetVersion(*resolvedBundle)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error getting resolved bundle version for bundle %q: %w", resolvedBundle.Name, err)
	}

	// Run validations against the resolved bundle to ensure only valid resolved bundles are being returned
	// Open Question: Should we grab the first valid bundle earlier?
	for _, validation := range r.Validations {
		if err := validation(resolvedBundle); err != nil {
			return nil, nil, nil, fmt.Errorf("validating bundle %q: %w", resolvedBundle.Name, err)
		}
	}

	return resolvedBundle, resolvedBundleVersion, priorDeprecation, nil
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

type CatalogWalkFunc func(context.Context, *catalogd.ClusterCatalog, *declcfg.DeclarativeConfig, error) error

func CatalogWalker(listCatalogs func(context.Context, ...client.ListOption) ([]catalogd.ClusterCatalog, error), getPackage func(context.Context, *catalogd.ClusterCatalog, string) (*declcfg.DeclarativeConfig, error)) func(ctx context.Context, packageName string, f CatalogWalkFunc, catalogListOpts ...client.ListOption) error {
	return func(ctx context.Context, packageName string, f CatalogWalkFunc, catalogListOpts ...client.ListOption) error {
		catalogs, err := listCatalogs(ctx, catalogListOpts...)
		if err != nil {
			return fmt.Errorf("error listing catalogs: %w", err)
		}

		for i := range catalogs {
			cat := &catalogs[i]
			fbc, fbcErr := getPackage(ctx, cat, packageName)
			if walkErr := f(ctx, cat, fbc, fbcErr); walkErr != nil {
				return walkErr
			}
		}
		return nil
	}
}
