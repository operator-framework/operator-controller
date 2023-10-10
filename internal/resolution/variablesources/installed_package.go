package variablesources

import (
	"context"
	"fmt"
	"sort"

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
	bundleImage   string
}

func (r *InstalledPackageVariableSource) GetVariables(ctx context.Context) ([]deppy.Variable, error) {
	allBundles, err := r.catalogClient.Bundles(ctx)
	if err != nil {
		return nil, err
	}

	// find corresponding bundle for the installed content
	resultSet := catalogfilter.Filter(allBundles, catalogfilter.WithBundleImage(r.bundleImage))
	if len(resultSet) == 0 {
		return nil, r.notFoundError()
	}

	// TODO: fast follow - we should check whether we are already supporting the channel attribute in the operator spec.
	//       if so, we should take the value from spec of the operator CR in the owner ref of the bundle deployment.
	//       If that channel is set, we need to update the filter above to filter by channel as well.
	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByVersion(resultSet[i], resultSet[j])
	})
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

func (r *InstalledPackageVariableSource) notFoundError() error {
	return fmt.Errorf("bundleImage %q not found", r.bundleImage)
}

func NewInstalledPackageVariableSource(catalogClient BundleProvider, bundleImage string) (*InstalledPackageVariableSource, error) {
	return &InstalledPackageVariableSource{
		catalogClient: catalogClient,
		bundleImage:   bundleImage,
		successors:    legacySemanticsSuccessors,
	}, nil
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
