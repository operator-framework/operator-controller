package variablesources

import (
	"context"
	"fmt"
	"sort"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	catalogclient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	"github.com/operator-framework/operator-controller/internal/resolution/variables"
)

var _ input.VariableSource = &InstalledPackageVariableSource{}

type InstalledPackageVariableSource struct {
	catalog     *catalogclient.Client
	bundleImage string
}

func (r *InstalledPackageVariableSource) GetVariables(ctx context.Context, _ input.EntitySource) ([]deppy.Variable, error) {
	allBundles, err := r.catalog.Bundles(ctx)
	if err != nil {
		return nil, err
	}

	// find corresponding bundle entity for the installed content
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

	// now find the bundles that replace the installed bundle
	// TODO: this algorithm does not yet consider skips and skipRange
	//       we simplify the process here by just searching for the bundle that replaces the installed bundle
	upgradeEdges := catalogfilter.Filter(allBundles, catalogfilter.Replaces(installedBundle.Name))
	sort.SliceStable(upgradeEdges, func(i, j int) bool {
		return catalogsort.ByVersion(upgradeEdges[i], upgradeEdges[j])
	})

	// you can always upgrade to yourself, i.e. not upgrade
	upgradeEdges = append(upgradeEdges, installedBundle)
	return []deppy.Variable{
		variables.NewInstalledPackageVariable(installedBundle.Package, upgradeEdges),
	}, nil
}

func (r *InstalledPackageVariableSource) notFoundError() error {
	return fmt.Errorf("bundleImage %q not found", r.bundleImage)
}

func NewInstalledPackageVariableSource(catalog *catalogclient.Client, bundleImage string) (*InstalledPackageVariableSource, error) {
	return &InstalledPackageVariableSource{
		catalog:     catalog,
		bundleImage: bundleImage,
	}, nil
}
