package variablesources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	"github.com/operator-framework/operator-controller/internal/resolution/util/predicates"
	"github.com/operator-framework/operator-controller/internal/resolution/util/sort"
	"github.com/operator-framework/operator-controller/internal/resolution/variables"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

var _ input.VariableSource = &InstalledPackageVariableSource{}

type InstalledPackageVariableSource struct {
	packageName string
	version     semver.Version
	channelName string
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
	validRange, err := semver.ParseRange(">=" + r.version.String())
	if err != nil {
		return nil, err
	}
	resultSet, err := entitySource.Filter(ctx, input.And(predicates.WithPackageName(r.packageName), predicates.InChannel(r.channelName), predicates.InSemverRange(validRange)))
	if err != nil {
		return nil, err
	}
	if len(resultSet) == 0 {
		return nil, r.notFoundError()
	}
	resultSet = resultSet.Sort(sort.ByChannelAndVersion)
	var bundleEntities []*olmentity.BundleEntity
	for i := 0; i < len(resultSet); i++ {

		replacesJSON := resultSet[i].Properties["olm.replaces"]
		packageJSON := resultSet[i].Properties["olm.package"]

		if replacesJSON == "" || packageJSON == "" {
			continue
		}

		// unmarshal replaces and packages
		var replaces replacesProperty
		var packages packageProperty
		if err := json.Unmarshal([]byte(replacesJSON), &replaces); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(packageJSON), &packages); err != nil {
			return nil, err
		}

		version, err := semver.Parse(packages.Version)
		if err != nil {
			return nil, err
		}

		expectedReplace := fmt.Sprintf("%s.v%s", r.packageName, r.version.String())
		if r.version.Equals(version) || replaces.Replaces == expectedReplace {
			bundleEntities = append(bundleEntities, olmentity.NewBundleEntity(&resultSet[i]))
		}

	}
	return []deppy.Variable{
		variables.NewInstalledPackageVariable(r.packageName, bundleEntities),
	}, nil
}

func (r *InstalledPackageVariableSource) notFoundError() error {
	return fmt.Errorf("package '%s' not installed", r.packageName)
}

func NewInstalledPackageVariableSource(bundleDeployment *rukpakv1alpha1.BundleDeployment) (*InstalledPackageVariableSource, error) {
	// TODO(jmprusi): proper if ... validation
	version := bundleDeployment.Annotations["operators.operatorframework.io/version"]
	channel := bundleDeployment.Annotations["operators.operatorframework.io/channel"]
	packageName := bundleDeployment.Annotations["operators.operatorframework.io/package"]

	if packageName == "" {
		return nil, fmt.Errorf("package name must not be empty")
	}

	semverVersion, err := semver.Parse(version)
	if err != nil {
		return nil, err
	}

	//TODO(jmprusi): check version and channel
	return &InstalledPackageVariableSource{
		packageName: packageName,
		version:     semverVersion,
		channelName: channel,
	}, nil
}
