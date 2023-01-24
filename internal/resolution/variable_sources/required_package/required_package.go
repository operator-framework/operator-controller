package required_package

import (
	"context"
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/utils/predicates"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/utils/sort"
)

type RequiredPackageVariable struct {
	*input.SimpleVariable
	bundleEntities []*variable_sources.BundleEntity
}

func (r *RequiredPackageVariable) BundleEntities() []*variable_sources.BundleEntity {
	return r.bundleEntities
}

func NewRequiredPackageVariable(packageName string, bundleEntities []*variable_sources.BundleEntity) *RequiredPackageVariable {
	id := deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))
	var entityIDs []deppy.Identifier
	for _, bundle := range bundleEntities {
		entityIDs = append(entityIDs, bundle.ID)
	}
	return &RequiredPackageVariable{
		SimpleVariable: input.NewSimpleVariable(id, constraint.Mandatory(), constraint.Dependency(entityIDs...)),
		bundleEntities: bundleEntities,
	}
}

var _ input.VariableSource = &RequiredPackageVariableSource{}

type RequiredPackageVariableSource struct {
	packageName string
}

func NewRequiredPackage(packageName string) *RequiredPackageVariableSource {
	requiredPackage := &RequiredPackageVariableSource{
		packageName: packageName,
	}
	return requiredPackage
}

func (r *RequiredPackageVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	resultSet, err := entitySource.Filter(ctx, predicates.WithPackageName(r.packageName))
	if err != nil {
		return nil, err
	}
	if len(resultSet) == 0 {
		return nil, fmt.Errorf("package '%s' not found", r.packageName)
	}
	resultSet = resultSet.Sort(sort.ByChannelAndVersion)
	var bundleEntities []*variable_sources.BundleEntity
	for i := 0; i < len(resultSet); i++ {
		bundleEntities = append(bundleEntities, variable_sources.NewBundleEntity(&resultSet[i]))
	}
	return []deppy.Variable{
		NewRequiredPackageVariable(r.packageName, bundleEntities),
	}, nil
}
