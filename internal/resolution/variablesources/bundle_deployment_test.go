package variablesources_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

func BundleDeploymentFakeClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(rukpakv1alpha1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func bundleDeployment(name, image string) *rukpakv1alpha1.BundleDeployment {
	return &rukpakv1alpha1.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: rukpakv1alpha1.BundleDeploymentSpec{
			ProvisionerClassName: "core-rukpak-io-plain",
			Template: &rukpakv1alpha1.BundleTemplate{
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: "core-rukpak-io-plain",
					Source: rukpakv1alpha1.BundleSource{
						Image: &rukpakv1alpha1.ImageSource{
							Ref: image,
						},
					},
				},
			},
		},
	}
}

var BundleDeploymentTestEntityCache = map[deppy.Identifier]input.Entity{
	"operatorhub/prometheus/0.37.0": *input.NewEntity("operatorhub/prometheus/0.37.0", map[string]string{
		"olm.bundle.path":         `"quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35"`,
		"olm.bundle.channelEntry": "{\"name\":\"prometheus.0.37.0\"}",
		"olm.channel":             "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.32.0\"}",
		"olm.gvk":                 "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"}, {\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"}]",
		"olm.package":             "{\"packageName\":\"prometheus\",\"version\":\"0.37.0\"}",
	}),
	"operatorhub/prometheus/0.47.0": *input.NewEntity("operatorhub/prometheus/0.47.0", map[string]string{
		"olm.bundle.path":         `"quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed"`,
		"olm.bundle.channelEntry": "{\"replaces\":\"prometheus.0.37.0\", \"name\":\"prometheus.0.47.0\"}",
		"olm.channel":             "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.37.0\"}",
		"olm.gvk":                 "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"}, {\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1alpha1\"}]",
		"olm.package":             "{\"packageName\":\"prometheus\",\"version\":\"0.47.0\"}",
	}),
	"operatorhub/packageA/2.0.0": *input.NewEntity("operatorhub/packageA/2.0.0", map[string]string{
		"olm.bundle.path":         `"foo.io/packageA/packageA:v2.0.0"`,
		"olm.bundle.channelEntry": "{\"name\":\"packageA.2.0.0\"}",
		"olm.channel":             "{\"channelName\":\"stable\",\"priority\":0}",
		"olm.gvk":                 "[{\"group\":\"foo.io\",\"kind\":\"Foo\",\"version\":\"v1\"}]",
		"olm.package":             "{\"packageName\":\"packageA\",\"version\":\"2.0.0\"}",
	}),
}

var _ = Describe("BundleDeploymentVariableSource", func() {
	var bundleTestEntityCache input.EntitySource

	BeforeEach(func() {
		bundleTestEntityCache = input.NewCacheQuerier(BundleDeploymentTestEntityCache)
	})

	It("should produce RequiredPackage variables", func() {
		cl := BundleDeploymentFakeClient(bundleDeployment("prometheus", "quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35"))

		bdVariableSource := variablesources.NewBundleDeploymentVariableSource(cl, &MockRequiredPackageSource{})
		variables, err := bdVariableSource.GetVariables(context.Background(), bundleTestEntityCache)
		Expect(err).ToNot(HaveOccurred())

		installedPackageVariable := filterVariables[*olmvariables.InstalledPackageVariable](variables)
		Expect(installedPackageVariable).To(HaveLen(1))
		Expect(installedPackageVariable).To(WithTransform(func(bvars []*olmvariables.InstalledPackageVariable) map[deppy.Identifier]int {
			out := map[deppy.Identifier]int{}
			for _, variable := range bvars {
				out[variable.Identifier()] = len(variable.BundleEntities())
			}
			return out
		}, Equal(map[deppy.Identifier]int{
			// Underlying `InstalledPackageVariableSource` returns current installed package
			// as a possible upgrade edge
			deppy.IdentifierFromString("installed package prometheus"): 2,
		})))
	})
	It("should return an error if the bundleDeployment image doesn't match any operator resource", func() {
		cl := BundleDeploymentFakeClient(bundleDeployment("prometheus", "quay.io/operatorhubio/prometheus@sha256:nonexistent"))

		bdVariableSource := variablesources.NewBundleDeploymentVariableSource(cl, &MockRequiredPackageSource{})
		_, err := bdVariableSource.GetVariables(context.Background(), bundleTestEntityCache)
		Expect(err.Error()).To(Equal("bundleImage \"quay.io/operatorhubio/prometheus@sha256:nonexistent\" not found"))
	})
})
