package variablesources_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

func FakeClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(operatorsv1alpha1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

var testEntityCache = map[deppy.Identifier]input.Entity{
	"operatorhub/prometheus/0.37.0": *input.NewEntity("operatorhub/prometheus/0.37.0", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35"`,
		"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.32.0\"}",
		"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"}, {\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"}]",
		"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.37.0\"}",
	}),
	"operatorhub/prometheus/0.47.0": *input.NewEntity("operatorhub/prometheus/0.47.0", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed"`,
		"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.37.0\"}",
		"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"}, {\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1alpha1\"}]",
		"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.47.0\"}",
	}),
	"operatorhub/packageA/2.0.0": *input.NewEntity("operatorhub/packageA/2.0.0", map[string]string{
		"olm.bundle.path": `"foo.io/packageA/packageA:v2.0.0"`,
		"olm.channel":     "{\"channelName\":\"stable\",\"priority\":0}",
		"olm.gvk":         "[{\"group\":\"foo.io\",\"kind\":\"Foo\",\"version\":\"v1\"}]",
		"olm.package":     "{\"packageName\":\"packageA\",\"version\":\"2.0.0\"}",
	}),
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
	var testEntitySource input.EntitySource

	BeforeEach(func() {
		testEntitySource = input.NewCacheQuerier(testEntityCache)
	})

	It("should produce RequiredPackage variables", func() {
		cl := FakeClient(operator("prometheus"), operator("packageA"))

		opVariableSource := variablesources.NewOperatorVariableSource(cl, &MockRequiredPackageSource{})
		variables, err := opVariableSource.GetVariables(context.Background(), testEntitySource)
		Expect(err).ToNot(HaveOccurred())

		packageRequiredVariables := filterVariables[*olmvariables.RequiredPackageVariable](variables)
		Expect(packageRequiredVariables).To(HaveLen(2))
		Expect(packageRequiredVariables).To(WithTransform(func(bvars []*olmvariables.RequiredPackageVariable) map[deppy.Identifier]int {
			out := map[deppy.Identifier]int{}
			for _, variable := range bvars {
				out[variable.Identifier()] = len(variable.BundleEntities())
			}
			return out
		}, Equal(map[deppy.Identifier]int{
			deppy.IdentifierFromString("required package prometheus"): 2,
			deppy.IdentifierFromString("required package packageA"):   1,
		})))
	})

	It("should return an errors when they occur", func() {
		cl := FakeClient(operator("prometheus"), operator("packageA"))

		opVariableSource := variablesources.NewOperatorVariableSource(cl, nil)
		_, err := opVariableSource.GetVariables(context.Background(), FailEntitySource{})
		Expect(err).To(HaveOccurred())
	})
})

var _ input.EntitySource = &FailEntitySource{}

type FailEntitySource struct {
}

func (f FailEntitySource) Get(_ context.Context, _ deppy.Identifier) (*input.Entity, error) {
	return nil, fmt.Errorf("error executing get")
}

func (f FailEntitySource) Filter(_ context.Context, _ input.Predicate) (input.EntityList, error) {
	return nil, fmt.Errorf("error executing filter")
}

func (f FailEntitySource) GroupBy(_ context.Context, _ input.GroupByFunction) (input.EntityListMap, error) {
	return nil, fmt.Errorf("error executing group by")
}

func (f FailEntitySource) Iterate(_ context.Context, _ input.IteratorFunction) error {
	return fmt.Errorf("error executing iterate")
}

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
