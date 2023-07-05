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

// TODO(jmprusi): move this somewhere else?
type replacesProperty struct {
	Replaces string `json:"replaces"`
}

// TODO(jmprusi): move this somewhere else?
type packageProperty struct {
	Package string `json:"packageName"`
	Version string `json:"version"`
}

func (r *InstalledPackageVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	resultSet, err := entitySource.Filter(ctx, input.And(predicates.WithBundleImage(r.bundleImage)))
	if err != nil {
		return nil, err
	}
	if len(resultSet) == 0 {
		return nil, r.notFoundError()
	}
	resultSet = resultSet.Sort(sort.ByChannelAndVersion)
	var bundleEntities []*olmentity.BundleEntity
	for i := 0; i < len(resultSet); i++ {
		bundleEntities = append(bundleEntities, olmentity.NewBundleEntity(&resultSet[i]))
	}
	return []deppy.Variable{
		variables.NewInstalledPackageVariable(r.bundleImage, bundleEntities),
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
