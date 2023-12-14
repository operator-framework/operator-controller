package controllers_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/pointer"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/controllers"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

func TestVariableSource(t *testing.T) {
	sch := runtime.NewScheme()
	utilruntime.Must(ocv1alpha1.AddToScheme(sch))
	utilruntime.Must(rukpakv1alpha1.AddToScheme(sch))

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

	bd := rukpakv1alpha1.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterExtensionName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         ocv1alpha1.GroupVersion.String(),
					Kind:               "ClusterExtension",
					Name:               clusterExtension.Name,
					UID:                clusterExtension.UID,
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				},
			},
		},
		Spec: rukpakv1alpha1.BundleDeploymentSpec{
			ProvisionerClassName: "core-rukpak-io-plain",
			Template: rukpakv1alpha1.BundleTemplate{
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: "core-rukpak-io-registry",
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeImage,
						Image: &rukpakv1alpha1.ImageSource{
							Ref: "foo.io/packageA/packageA:v2.0.0",
						},
					},
				},
			},
		},
	}

	vars, err := controllers.GenerateVariables(allBundles, []ocv1alpha1.ClusterExtension{clusterExtension}, []rukpakv1alpha1.BundleDeployment{bd})
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
