package controllers_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/controllers"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

func TestVariableSource(t *testing.T) {
	stableChannel := catalogmetadata.Channel{Channel: declcfg.Channel{
		Name: "stable",
		Entries: []declcfg.ChannelEntry{
			{
				Name: "packageA.v2.0.0",
			},
		},
	}}
	bundleSet := map[string]*catalogmetadata.Bundle{
		"packageA.v2.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "packageA.v2.0.0",
				Package: "packageA",
				Image:   "foo.io/packageA/packageA:v2.0.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"packageA","version":"2.0.0"}`)},
				},
			},
			CatalogName: "fake-catalog",
			InChannels:  []*catalogmetadata.Channel{&stableChannel},
		},
	}
	allBundles := make([]*catalogmetadata.Bundle, 0, len(bundleSet))
	for _, bundle := range bundleSet {
		allBundles = append(allBundles, bundle)
	}

	pkgName := "packageA"
	clusterExtensionName := fmt.Sprintf("clusterextension-test-%s", rand.String(8))
	clusterExtension := ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: clusterExtensionName},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName: pkgName,
			Channel:     "stable",
			Version:     "2.0.0",
		},
	}

	bd := rukpakv1alpha2.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterExtensionName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         ocv1alpha1.GroupVersion.String(),
					Kind:               "ClusterExtension",
					Name:               clusterExtension.Name,
					UID:                clusterExtension.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: rukpakv1alpha2.BundleDeploymentSpec{
			ProvisionerClassName: "core-rukpak-io-registry",
			Source: rukpakv1alpha2.BundleSource{
				Type: rukpakv1alpha2.SourceTypeImage,
				Image: &rukpakv1alpha2.ImageSource{
					Ref: "foo.io/packageA/packageA:v2.0.0",
				},
			},
		},
	}

	vars, err := controllers.GenerateVariables(allBundles, []ocv1alpha1.ClusterExtension{clusterExtension}, []rukpakv1alpha2.BundleDeployment{bd})
	require.NoError(t, err)

	expectedVars := []deppy.Variable{
		olmvariables.NewRequiredPackageVariable("packageA", []*catalogmetadata.Bundle{
			bundleSet["packageA.v2.0.0"],
		}),
		olmvariables.NewInstalledPackageVariable("packageA", []*catalogmetadata.Bundle{
			bundleSet["packageA.v2.0.0"],
		}),
		olmvariables.NewBundleVariable(bundleSet["packageA.v2.0.0"], nil),
		olmvariables.NewBundleUniquenessVariable(
			"packageA package uniqueness",
			deppy.Identifier("fake-catalog-packageA-packageA.v2.0.0"),
		),
	}
	gocmpopts := []cmp.Option{
		cmpopts.IgnoreUnexported(catalogmetadata.Bundle{}),
		cmp.AllowUnexported(
			olmvariables.RequiredPackageVariable{},
			olmvariables.InstalledPackageVariable{},
			olmvariables.BundleVariable{},
			olmvariables.BundleUniquenessVariable{},
			input.SimpleVariable{},
			constraint.DependencyConstraint{},
			constraint.AtMostConstraint{},
		),
	}
	require.Empty(t, cmp.Diff(vars, expectedVars, gocmpopts...))
}
