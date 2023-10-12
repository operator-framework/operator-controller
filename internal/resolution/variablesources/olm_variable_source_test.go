package variablesources_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
	testutil "github.com/operator-framework/operator-controller/test/util"
)

func TestOLMVariableSource(t *testing.T) {
	sch := runtime.NewScheme()
	utilruntime.Must(operatorsv1alpha1.AddToScheme(sch))
	utilruntime.Must(rukpakv1alpha1.AddToScheme(sch))

	stableChannel := catalogmetadata.Channel{Channel: declcfg.Channel{
		Name: "stable",
		Entries: []declcfg.ChannelEntry{
			{
				Name: "packageA.v2.0.0",
			},
		},
	}}
	testBundleList := []*catalogmetadata.Bundle{
		{
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

	pkgName := "packageA"
	opName := fmt.Sprintf("operator-test-%s", rand.String(8))
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opName},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: pkgName,
			Channel:     "stable",
			Version:     "2.0.0",
		},
	}

	bd := &rukpakv1alpha1.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: opName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         operatorsv1alpha1.GroupVersion.String(),
					Kind:               "Operator",
					Name:               operator.Name,
					UID:                operator.UID,
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				},
			},
		},
		Spec: rukpakv1alpha1.BundleDeploymentSpec{
			ProvisionerClassName: "core-rukpak-io-plain",
			Template: &rukpakv1alpha1.BundleTemplate{
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
	fakeClient := fake.NewClientBuilder().WithScheme(sch).WithObjects(operator, bd).Build()
	fakeCatalogClient := testutil.NewFakeCatalogClient(testBundleList)

	vs := variablesources.NewOLMVariableSource(fakeClient, &fakeCatalogClient)

	vars, err := vs.GetVariables(context.Background())
	require.NoError(t, err)

	require.Len(t, vars, 4)
	assert.Equal(t, "required package packageA", vars[0].Identifier().String())
	assert.IsType(t, &olmvariables.RequiredPackageVariable{}, vars[0])
	assert.Equal(t, "installed package packageA", vars[1].Identifier().String())
	assert.IsType(t, &olmvariables.InstalledPackageVariable{}, vars[1])
	assert.Equal(t, "fake-catalog-packageA-packageA.v2.0.0", vars[2].Identifier().String())
	assert.IsType(t, &olmvariables.BundleVariable{}, vars[2])
	assert.Equal(t, "packageA package uniqueness", vars[3].Identifier().String())
	assert.IsType(t, &olmvariables.BundleUniquenessVariable{}, vars[3])
}
