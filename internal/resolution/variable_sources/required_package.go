package variable_sources

import (
	"context"
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

type RequiredPackageVariable struct {
	*input.SimpleVariable
	bundleEntities []*BundleEntity
}

func (r *RequiredPackageVariable) BundleEntities() []*BundleEntity {
	return r.bundleEntities
}

func NewRequiredPackageVariable(packageName string, bundleEntities []*BundleEntity) *RequiredPackageVariable {
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
	resultSet, err := entitySource.Filter(ctx, WithPackageName(r.packageName))
	if err != nil {
		return nil, err
	}
	if len(resultSet) == 0 {
		return nil, fmt.Errorf("package '%s' not found", r.packageName)
	}
	resultSet = resultSet.Sort(ByChannelAndVersion)
	var bundleEntities []*BundleEntity
	for i := 0; i < len(resultSet); i++ {
		bundleEntities = append(bundleEntities, NewBundleEntity(&resultSet[i]))
	}
	return []deppy.Variable{
		NewRequiredPackageVariable(r.packageName, bundleEntities),
	}, nil
}
