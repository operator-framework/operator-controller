package variablesources

import (
	"fmt"
	"sort"

	mmsemver "github.com/Masterminds/semver/v3"
	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/pkg/features"
)

// MakeInstalledPackageVariables returns variables representing packages
// already installed in the system.
// Meaning that each BundleDeployment managed by operator-controller
// has own variable.
func MakeInstalledPackageVariables(
	allBundles []*catalogmetadata.Bundle,
	clusterExtensions []ocv1alpha1.ClusterExtension,
	bundleDeployments []rukpakv1alpha2.BundleDeployment,
) ([]*olmvariables.InstalledPackageVariable, error) {
	var successors successorsFunc = legacySemanticsSuccessors
	if features.OperatorControllerFeatureGate.Enabled(features.ForceSemverUpgradeConstraints) {
		successors = semverSuccessors
	}

	ownerIDToBundleDeployment := mapOwnerIDToBundleDeployment(bundleDeployments)

	result := make([]*olmvariables.InstalledPackageVariable, 0, len(clusterExtensions))
	processed := sets.Set[string]{}
	for _, clusterExtension := range clusterExtensions {
		if clusterExtension.Spec.UpgradeConstraintPolicy == ocv1alpha1.UpgradeConstraintPolicyIgnore {
			continue
		}

		bundleDeployment, ok := ownerIDToBundleDeployment[clusterExtension.UID]
		if !ok {
			// This can happen when an ClusterExtension is requested,
			// but not yet installed (e.g. no BundleDeployment created for it)
			continue
		}

		sourceImage := bundleDeployment.Spec.Source.Image
		if sourceImage == nil || sourceImage.Ref == "" {
			continue
		}

		if processed.Has(sourceImage.Ref) {
			continue
		}
		processed.Insert(sourceImage.Ref)

		bundleImage := sourceImage.Ref

		// find corresponding bundle for the installed content
		resultSet := catalogfilter.Filter(allBundles, catalogfilter.And(
			catalogfilter.WithPackageName(clusterExtension.Spec.PackageName),
			catalogfilter.WithBundleImage(bundleImage),
		))
		if len(resultSet) == 0 {
			return nil, fmt.Errorf("bundle with image %q for package %q not found in available catalogs but is currently installed via BundleDeployment %q", bundleImage, clusterExtension.Spec.PackageName, bundleDeployment.Name)
		}

		sort.SliceStable(resultSet, func(i, j int) bool {
			return catalogsort.ByVersion(resultSet[i], resultSet[j])
		})
		installedBundle := resultSet[0]

		upgradeEdges, err := successors(allBundles, installedBundle)
		if err != nil {
			return nil, err
		}

		// you can always upgrade to yourself, i.e. not upgrade
		upgradeEdges = append(upgradeEdges, installedBundle)
		result = append(result, olmvariables.NewInstalledPackageVariable(installedBundle.Package, upgradeEdges))
	}

	return result, nil
}

// successorsFunc must return successors of a currently installed bundle
// from a list of all bundles provided to the function.
// Must not return installed bundle as a successor
type successorsFunc func(allBundles []*catalogmetadata.Bundle, installedBundle *catalogmetadata.Bundle) ([]*catalogmetadata.Bundle, error)

// legacySemanticsSuccessors returns successors based on legacy OLMv0 semantics
// which rely on Replaces, Skips and skipRange.
func legacySemanticsSuccessors(allBundles []*catalogmetadata.Bundle, installedBundle *catalogmetadata.Bundle) ([]*catalogmetadata.Bundle, error) {
	// find the bundles that replace, skip, or skipRange the bundle provided
	upgradeEdges := catalogfilter.Filter(allBundles, catalogfilter.And(
		catalogfilter.WithPackageName(installedBundle.Package),
		catalogfilter.LegacySuccessor(installedBundle),
	))
	sort.SliceStable(upgradeEdges, func(i, j int) bool {
		return catalogsort.ByVersion(upgradeEdges[i], upgradeEdges[j])
	})

	return upgradeEdges, nil
}

// semverSuccessors returns successors based on Semver.
// Successors will not include versions outside the major version of the
// installed bundle as major version is intended to indicate breaking changes.
func semverSuccessors(allBundles []*catalogmetadata.Bundle, installedBundle *catalogmetadata.Bundle) ([]*catalogmetadata.Bundle, error) {
	currentVersion, err := installedBundle.Version()
	if err != nil {
		return nil, err
	}

	// Based on current version create a caret range comparison constraint
	// to allow only minor and patch version as successors and exclude current version.
	constraintStr := fmt.Sprintf("^%s, != %s", currentVersion.String(), currentVersion.String())
	wantedVersionRangeConstraint, err := mmsemver.NewConstraint(constraintStr)
	if err != nil {
		return nil, err
	}

	upgradeEdges := catalogfilter.Filter(allBundles, catalogfilter.And(
		catalogfilter.WithPackageName(installedBundle.Package),
		catalogfilter.InMastermindsSemverRange(wantedVersionRangeConstraint),
	))
	sort.SliceStable(upgradeEdges, func(i, j int) bool {
		return catalogsort.ByVersion(upgradeEdges[i], upgradeEdges[j])
	})

	return upgradeEdges, nil
}

func mapOwnerIDToBundleDeployment(bundleDeployments []rukpakv1alpha2.BundleDeployment) map[types.UID]*rukpakv1alpha2.BundleDeployment {
	result := map[types.UID]*rukpakv1alpha2.BundleDeployment{}

	for idx := range bundleDeployments {
		for _, ref := range bundleDeployments[idx].OwnerReferences {
			result[ref.UID] = &bundleDeployments[idx]
		}
	}

	return result
}
