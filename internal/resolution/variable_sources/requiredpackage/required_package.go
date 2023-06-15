package requiredpackage

import (
	"context"
	"fmt"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/util/predicates"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/util/sort"
)

type RequiredPackageVariable struct {
	*input.SimpleVariable
	bundleEntities []*olmentity.BundleEntity
}

func (r *RequiredPackageVariable) BundleEntities() []*olmentity.BundleEntity {
	return r.bundleEntities
}

func NewRequiredPackageVariable(packageName string, bundleEntities []*olmentity.BundleEntity) *RequiredPackageVariable {
	id := deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))
	entityIDs := make([]deppy.Identifier, 0, len(bundleEntities))
	for _, bundle := range bundleEntities {
		entityIDs = append(entityIDs, bundle.ID)
	}
	return &RequiredPackageVariable{
		SimpleVariable: input.NewSimpleVariable(id, constraint.Mandatory(), constraint.Dependency(entityIDs...)),
		bundleEntities: bundleEntities,
	}
}

var _ input.VariableSource = &RequiredPackageVariableSource{}

type RequiredPackageOption func(*RequiredPackageVariableSource) error

func InVersionRange(versionRange string) RequiredPackageOption {
	return func(r *RequiredPackageVariableSource) error {
		if versionRange != "" {
			vr, err := semver.ParseRange(versionRange)
			if err == nil {
				r.versionRange = versionRange
				r.predicates = append(r.predicates, predicates.InSemverRange(vr))
				return nil
			}

			return fmt.Errorf("invalid version range '%s': %v", versionRange, err)
		}
		return nil
	}
}

func InChannel(channelName string) RequiredPackageOption {
	return func(r *RequiredPackageVariableSource) error {
		if channelName != "" {
			r.channelName = channelName
			r.predicates = append(r.predicates, predicates.InChannel(channelName))
		}
		return nil
	}
}

type RequiredPackageVariableSource struct {
	packageName  string
	versionRange string
	channelName  string
	predicates   []input.Predicate
}

func NewRequiredPackage(packageName string, options ...RequiredPackageOption) (*RequiredPackageVariableSource, error) {
	if packageName == "" {
		return nil, fmt.Errorf("package name must not be empty")
	}
	r := &RequiredPackageVariableSource{
		packageName: packageName,
		predicates:  []input.Predicate{predicates.WithPackageName(packageName)},
	}
	for _, option := range options {
		if err := option(r); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *RequiredPackageVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	resultSet, err := entitySource.Filter(ctx, input.And(r.predicates...))
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
		NewRequiredPackageVariable(r.packageName, bundleEntities),
	}, nil
}

func (r *RequiredPackageVariableSource) notFoundError() error {
	// TODO: update this error message when/if we decide to support version ranges as opposed to fixing the version
	//  context: we originally wanted to support version ranges and take the highest version that satisfies the range
	//  during the upstream call on the 2023-04-11 we decided to pin the version instead. But, we'll keep version range
	//  support under the covers in case we decide to pivot back.
	if r.versionRange != "" && r.channelName != "" {
		return fmt.Errorf("package '%s' at version '%s' in channel '%s' not found", r.packageName, r.versionRange, r.channelName)
	}
	if r.versionRange != "" {
		return fmt.Errorf("package '%s' at version '%s' not found", r.packageName, r.versionRange)
	}
	if r.channelName != "" {
		return fmt.Errorf("package '%s' in channel '%s' not found", r.packageName, r.channelName)
	}
	return fmt.Errorf("package '%s' not found", r.packageName)
}
