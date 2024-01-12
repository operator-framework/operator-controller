package variablesources

import (
	"fmt"
	"sort"

	mmsemver "github.com/Masterminds/semver/v3"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

// MakeRequiredPackageVariables returns a variable which represent
// explicit requirement for a package from an user.
// This is when a user explicitly asks "install this" via ClusterExtension API.
func MakeRequiredPackageVariables(allBundles []*catalogmetadata.Bundle, clusterExtensions []ocv1alpha1.ClusterExtension) ([]*olmvariables.RequiredPackageVariable, error) {
	result := make([]*olmvariables.RequiredPackageVariable, 0, len(clusterExtensions))

	for _, clusterExtension := range clusterExtensions {
		packageName := clusterExtension.Spec.PackageName
		channelName := clusterExtension.Spec.Channel
		versionRange := clusterExtension.Spec.Version

		predicates := []catalogfilter.Predicate[catalogmetadata.Bundle]{
			catalogfilter.WithPackageName(packageName),
		}

		if channelName != "" {
			predicates = append(predicates, catalogfilter.InChannel(channelName))
		}

		if versionRange != "" {
			vr, err := mmsemver.NewConstraint(versionRange)
			if err != nil {
				return nil, fmt.Errorf("invalid version range %q: %w", versionRange, err)
			}
			predicates = append(predicates, catalogfilter.InMastermindsSemverRange(vr))
		}

		resultSet := catalogfilter.Filter(allBundles, catalogfilter.And(predicates...))
		if len(resultSet) == 0 {
			if versionRange != "" && channelName != "" {
				return nil, fmt.Errorf("no package %q matching version %q found in channel %q", packageName, versionRange, channelName)
			}
			if versionRange != "" {
				return nil, fmt.Errorf("no package %q matching version %q found", packageName, versionRange)
			}
			if channelName != "" {
				return nil, fmt.Errorf("no package %q found in channel %q", packageName, channelName)
			}
			return nil, fmt.Errorf("no package %q found", packageName)
		}
		sort.SliceStable(resultSet, func(i, j int) bool {
			return catalogsort.ByVersion(resultSet[i], resultSet[j])
		})
		sort.SliceStable(resultSet, func(i, j int) bool {
			return catalogsort.ByDeprecated(resultSet[i], resultSet[j])
		})

		result = append(result, olmvariables.NewRequiredPackageVariable(packageName, resultSet))
	}

	return result, nil
}
