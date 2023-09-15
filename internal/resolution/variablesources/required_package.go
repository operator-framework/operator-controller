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
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

var _ input.VariableSource = &RequiredPackageVariableSource{}

type RequiredPackageVariableSourceOption func(*RequiredPackageVariableSource) error

func InVersionRange(versionRange string) RequiredPackageVariableSourceOption {
	return func(r *RequiredPackageVariableSource) error {
		if versionRange != "" {
			vr, err := mmsemver.NewConstraint(versionRange)
			if err == nil {
				r.versionRange = versionRange
				r.predicates = append(r.predicates, catalogfilter.InMastermindsSemverRange(vr))
				return nil
			}

			return fmt.Errorf("invalid version range '%s': %w", versionRange, err)
		}
		return nil
	}
}

func InChannel(channelName string) RequiredPackageVariableSourceOption {
	return func(r *RequiredPackageVariableSource) error {
		if channelName != "" {
			r.channelName = channelName
			r.predicates = append(r.predicates, catalogfilter.InChannel(channelName))
		}
		return nil
	}
}

type RequiredPackageVariableSource struct {
	catalogClient BundleProvider

	packageName  string
	versionRange string
	channelName  string
	predicates   []catalogfilter.Predicate[catalogmetadata.Bundle]
}

func NewRequiredPackageVariableSource(catalogClient BundleProvider, packageName string, options ...RequiredPackageVariableSourceOption) (*RequiredPackageVariableSource, error) {
	if packageName == "" {
		return nil, fmt.Errorf("package name must not be empty")
	}
	r := &RequiredPackageVariableSource{
		catalogClient: catalogClient,

		packageName: packageName,
		predicates:  []catalogfilter.Predicate[catalogmetadata.Bundle]{catalogfilter.WithPackageName(packageName)},
	}
	for _, option := range options {
		if err := option(r); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *RequiredPackageVariableSource) GetVariables(ctx context.Context) ([]deppy.Variable, error) {
	resultSet, err := r.catalogClient.Bundles(ctx)
	if err != nil {
		return nil, err
	}

	resultSet = catalogfilter.Filter(resultSet, catalogfilter.And(r.predicates...))
	if len(resultSet) == 0 {
		return nil, r.notFoundError()
	}
	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByVersion(resultSet[i], resultSet[j])
	})
	return []deppy.Variable{
		olmvariables.NewRequiredPackageVariable(r.packageName, resultSet),
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
