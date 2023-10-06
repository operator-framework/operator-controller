package variablesources_test

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	. "github.com/onsi/ginkgo/v2"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	testutil "github.com/operator-framework/operator-controller/test/util"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

func FakeClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(operatorsv1alpha1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func operator(name string) *operatorsv1alpha1.Operator {
	return &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: name,
		},
	}
}

var _ = Describe("OperatorVariableSource", func() {
	var betaChannel catalogmetadata.Channel
	var stableChannel catalogmetadata.Channel
	var testBundleList []*catalogmetadata.Bundle

	BeforeEach(func() {
		betaChannel = catalogmetadata.Channel{
			Channel: declcfg.Channel{
				Name: "beta",
				Entries: []declcfg.ChannelEntry{
					{
						Name:     "operatorhub/prometheus/0.37.0",
						Replaces: "operatorhub/prometheus/0.32.0",
					},
					{
						Name:     "operatorhub/prometheus/0.47.0",
						Replaces: "operatorhub/prometheus/0.37.0",
					},
				},
			},
		}

		stableChannel = catalogmetadata.Channel{
			Channel: declcfg.Channel{
				Name: "stable",
				Entries: []declcfg.ChannelEntry{
					{
						Name: "operatorhub/packageA/2.0.0",
					},
				},
			},
		}

		testBundleList = []*catalogmetadata.Bundle{
			{Bundle: declcfg.Bundle{
				Name:    "operatorhub/prometheus/0.37.0",
				Package: "prometheus",
				Image:   "quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"0.37.0"}`)},
					{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"monitoring.coreos.com","kind":"Alertmanager","version":"v1"}, {"group":"monitoring.coreos.com","kind":"Prometheus","version":"v1"}]`)},
				}},
				InChannels: []*catalogmetadata.Channel{&betaChannel},
			},
			{Bundle: declcfg.Bundle{
				Name:    "operatorhub/prometheus/0.47.0",
				Package: "prometheus",
				Image:   "quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"0.47.0"}`)},
					{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"monitoring.coreos.com","kind":"Alertmanager","version":"v1"}, {"group":"monitoring.coreos.com","kind":"Prometheus","version":"v1alpha1"}]`)},
				}},
				InChannels: []*catalogmetadata.Channel{&betaChannel},
			},
			{Bundle: declcfg.Bundle{
				Name:    "operatorhub/packageA/2.0.0",
				Package: "packageA",
				Image:   "foo.io/packageA/packageA:v2.0.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"packageA","version":"2.0.0"}`)},
					{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
				}},
				InChannels: []*catalogmetadata.Channel{&stableChannel},
			},
		}

	})

	It("should produce RequiredPackage variables", func() {
		cl := FakeClient(operator("prometheus"), operator("packageA"))
		fakeCatalogClient := testutil.NewFakeCatalogClient(testBundleList)
		opVariableSource := variablesources.NewOperatorVariableSource(cl, &fakeCatalogClient, &MockVariableSource{})
		variables, err := opVariableSource.GetVariables(context.Background())
		Expect(err).ToNot(HaveOccurred())

		packageRequiredVariables := filterVariables[*olmvariables.RequiredPackageVariable](variables)
		Expect(packageRequiredVariables).To(HaveLen(2))
		Expect(packageRequiredVariables).To(WithTransform(func(bvars []*olmvariables.RequiredPackageVariable) map[deppy.Identifier]int {
			out := map[deppy.Identifier]int{}
			for _, variable := range bvars {
				out[variable.Identifier()] = len(variable.Bundles())
			}
			return out
		}, Equal(map[deppy.Identifier]int{
			deppy.IdentifierFromString("required package prometheus"): 2,
			deppy.IdentifierFromString("required package packageA"):   1,
		})))
	})

	It("should return an errors when they occur", func() {
		cl := FakeClient(operator("prometheus"), operator("packageA"))
		fakeCatalogClient := testutil.NewFakeCatalogClientWithError(errors.New("something bad happened"))

		opVariableSource := variablesources.NewOperatorVariableSource(cl, &fakeCatalogClient, nil)
		_, err := opVariableSource.GetVariables(context.Background())
		Expect(err).To(HaveOccurred())
	})
})

func filterVariables[D deppy.Variable](variables []deppy.Variable) []D {
	var out []D
	for _, variable := range variables {
		switch v := variable.(type) {
		case D:
			out = append(out, v)
		}
	}
	return out
}
