package variablesources

import (
	"context"
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	"github.com/operator-framework/operator-controller/internal/resolution/util/predicates"
	"github.com/operator-framework/operator-controller/internal/resolution/util/sort"
	"github.com/operator-framework/operator-controller/internal/resolution/variables"
)

var _ input.VariableSource = &InstalledPackageVariableSource{}

type InstalledPackageVariableSource struct {
	bundleImage string
}

func (r *InstalledPackageVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	// find corresponding bundle entity for the installed content
	resultSet, err := entitySource.Filter(ctx, predicates.WithBundleImage(r.bundleImage))
	if err != nil {
		return nil, err
	}
	if len(resultSet) == 0 {
		return nil, r.notFoundError()
	}

	// sort by channel and version
	// TODO: this is a bit of a hack and it assumes a well formed catalog.
	//       we currently have one entity per bundle/channel, i.e. if a bundle
	//       appears in multiple channels, we have multiple entities for it.
	//       this means that for a well formed catalog, we could get multiple entities
	//       back as a response to the filter above. For now, we sort by channel and version
	//       and take the top most element. Soon, we will add package and channel variables making
	//       this unnecessary.
	// TODO: fast follow - we should check whether we are already supporting the channel attribute in the operator spec.
	//       if so, we should take the value from spec of the operator CR in the owner ref of the bundle deployment.
	//       If that channel is set, we need to update the filter above to filter by channel as well.
	resultSet = resultSet.Sort(sort.ByChannelAndVersion)
	installedBundle := olmentity.NewBundleEntity(&resultSet[0])

	// now find the bundles that replace the installed bundle
	// TODO: this algorithm does not yet consider skips and skipRange
	//       we simplify the process here by just searching for the bundle that replaces the installed bundle
	packageName, err := installedBundle.PackageName()
	if err != nil {
		return nil, err
	}
	version, err := installedBundle.Version()
	if err != nil {
		return nil, err
	}
	bundleID := fmt.Sprintf("%s.v%s", packageName, version.String())
	resultSet, err = entitySource.Filter(ctx, predicates.Replaces(bundleID))
	if err != nil {
		return nil, err
	}
	resultSet = resultSet.Sort(sort.ByChannelAndVersion)
	upgradeEdges := make([]*olmentity.BundleEntity, 0, len(resultSet))
	for i := range resultSet {
		upgradeEdges = append(upgradeEdges, olmentity.NewBundleEntity(&resultSet[i]))
	}

	// you can always upgrade to yourself, i.e. not upgrade
	upgradeEdges = append(upgradeEdges, installedBundle)
	return []deppy.Variable{
		variables.NewInstalledPackageVariable(bundleID, upgradeEdges),
	}, nil
}

func (r *InstalledPackageVariableSource) notFoundError() error {
	return fmt.Errorf("bundleImage %q not found", r.bundleImage)
}

func NewInstalledPackageVariableSource(bundleImage string) (*InstalledPackageVariableSource, error) {
	return &InstalledPackageVariableSource{
		bundleImage: bundleImage,
	}, nil
}
