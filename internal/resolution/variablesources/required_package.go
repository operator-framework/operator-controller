package variablesources

import (
	"fmt"
	"sort"

	mmsemver "github.com/Masterminds/semver/v3"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

// MakeRequiredPackageVariables returns a variable which represent
// explicit requirement for a package from an user.
// This is when an user explicitly asks "install this" via Operator API.
func MakeRequiredPackageVariables(allBundles []*catalogmetadata.Bundle, operators []operatorsv1alpha1.Operator) ([]*olmvariables.RequiredPackageVariable, error) {
	result := make([]*olmvariables.RequiredPackageVariable, 0, len(operators))

	for _, operator := range operators {
		packageName := operator.Spec.PackageName
		channelName := operator.Spec.Channel
		versionRange := operator.Spec.Version

		predicates := []catalogfilter.Predicate[catalogmetadata.Bundle]{
			catalogfilter.WithPackageName(packageName),
		}

		if channelName != "" {
			predicates = append(predicates, catalogfilter.InChannel(channelName))
		}

		if versionRange != "" {
			vr, err := mmsemver.NewConstraint(versionRange)
			if err != nil {
				return nil, fmt.Errorf("invalid version range '%s': %w", versionRange, err)
			}
			predicates = append(predicates, catalogfilter.InMastermindsSemverRange(vr))
		}

		resultSet := catalogfilter.Filter(allBundles, catalogfilter.And(predicates...))
		if len(resultSet) == 0 {
			if versionRange != "" && channelName != "" {
				return nil, fmt.Errorf("no package '%s' matching version '%s' found in channel '%s'", packageName, versionRange, channelName)
			}
			if versionRange != "" {
				return nil, fmt.Errorf("no package '%s' matching version '%s' found", packageName, versionRange)
			}
			if channelName != "" {
				return nil, fmt.Errorf("no package '%s' found in channel '%s'", packageName, channelName)
			}
			return nil, fmt.Errorf("no package '%s' found", packageName)
		}
		sort.SliceStable(resultSet, func(i, j int) bool {
			return catalogsort.ByVersion(resultSet[i], resultSet[j])
		})

		result = append(result, olmvariables.NewRequiredPackageVariable(packageName, resultSet))
	}

	return result, nil
}
