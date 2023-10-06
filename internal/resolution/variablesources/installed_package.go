package variablesources

import (
	"context"
	"fmt"
	"sort"

	mmsemver "github.com/Masterminds/semver/v3"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	"github.com/operator-framework/operator-controller/internal/resolution/variables"
)

var _ input.VariableSource = &InstalledPackageVariableSource{}

type InstalledPackageVariableSource struct {
	catalogClient BundleProvider
	successors    successorsFunc
	pkgName       string
	bundleName    string
	bundleVersion string
}

func (r *InstalledPackageVariableSource) GetVariables(ctx context.Context) ([]deppy.Variable, error) {
	allBundles, err := r.catalogClient.Bundles(ctx)
	if err != nil {
		return nil, err
	}

	vr, err := mmsemver.NewConstraint(r.bundleVersion)
	if err != nil {
		return nil, err
	}

	// find corresponding bundle for the installed content
	resultSet := catalogfilter.Filter(allBundles, catalogfilter.And(
		catalogfilter.WithPackageName(r.pkgName),
		catalogfilter.WithName(r.bundleName),
		catalogfilter.InMastermindsSemverRange(vr),
	))
	if len(resultSet) == 0 {
		return nil, fmt.Errorf("bundle for package %q with name %q at version %q not found", r.pkgName, r.bundleName, r.bundleVersion)
	}
	if len(resultSet) > 1 {
		return nil, fmt.Errorf("more than one bundle for package %q with name %q at version %q found", r.pkgName, r.bundleName, r.bundleVersion)
	}
	installedBundle := resultSet[0]

	upgradeEdges, err := r.successors(allBundles, installedBundle)
	if err != nil {
		return nil, err
	}

	// you can always upgrade to yourself, i.e. not upgrade
	upgradeEdges = append(upgradeEdges, installedBundle)
	return []deppy.Variable{
		variables.NewInstalledPackageVariable(installedBundle.Package, upgradeEdges),
	}, nil
}

func NewInstalledPackageVariableSource(catalogClient BundleProvider, pkgName, bundleName, bundleVersion string) *InstalledPackageVariableSource {
	return &InstalledPackageVariableSource{
		catalogClient: catalogClient,
		successors:    legacySemanticsSuccessors,
		pkgName:       pkgName,
		bundleName:    bundleName,
		bundleVersion: bundleVersion,
	}
}

// successorsFunc must return successors of a currently installed bundle
// from a list of all bundles provided to the function.
// Must not return installed bundle as a successor
type successorsFunc func(allBundles []*catalogmetadata.Bundle, installedBundle *catalogmetadata.Bundle) ([]*catalogmetadata.Bundle, error)

// legacySemanticsSuccessors returns successors based on legacy OLMv0 semantics
// which rely on Replaces, Skips and skipRange.
func legacySemanticsSuccessors(allBundles []*catalogmetadata.Bundle, installedBundle *catalogmetadata.Bundle) ([]*catalogmetadata.Bundle, error) {
	// find the bundles that replace the bundle provided
	// TODO: this algorithm does not yet consider skips and skipRange
	upgradeEdges := catalogfilter.Filter(allBundles, catalogfilter.Replaces(installedBundle.Name))
	sort.SliceStable(upgradeEdges, func(i, j int) bool {
		return catalogsort.ByVersion(upgradeEdges[i], upgradeEdges[j])
	})

	return upgradeEdges, nil
}
