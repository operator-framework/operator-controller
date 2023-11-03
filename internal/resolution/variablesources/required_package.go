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
	allBundles []*catalogmetadata.Bundle

	packageName  string
	versionRange string
	channelName  string
	predicates   []catalogfilter.Predicate[catalogmetadata.Bundle]
}

func NewRequiredPackageVariableSource(allBundles []*catalogmetadata.Bundle, packageName string, options ...RequiredPackageVariableSourceOption) (*RequiredPackageVariableSource, error) {
	if packageName == "" {
		return nil, fmt.Errorf("package name must not be empty")
	}
	r := &RequiredPackageVariableSource{
		allBundles: allBundles,

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

func (r *RequiredPackageVariableSource) GetVariables(_ context.Context) ([]deppy.Variable, error) {
	resultSet := catalogfilter.Filter(r.allBundles, catalogfilter.And(r.predicates...))
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
	if r.versionRange != "" && r.channelName != "" {
		return fmt.Errorf("no package '%s' matching version '%s' found in channel '%s'", r.packageName, r.versionRange, r.channelName)
	}
	if r.versionRange != "" {
		return fmt.Errorf("no package '%s' matching version '%s' found", r.packageName, r.versionRange)
	}
	if r.channelName != "" {
		return fmt.Errorf("no package '%s' found in channel '%s'", r.packageName, r.channelName)
	}
	return fmt.Errorf("no package '%s' found", r.packageName)
}
