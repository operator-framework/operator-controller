package variablesources_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/deppy/pkg/deppy"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

func bundleDeployment(name, image string) rukpakv1alpha1.BundleDeployment {
	return rukpakv1alpha1.BundleDeployment{
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

var _ = Describe("BundleDeploymentVariableSource", func() {
	var betaChannel catalogmetadata.Channel
	var stableChannel catalogmetadata.Channel
	var testBundleList []*catalogmetadata.Bundle

	BeforeEach(func() {
		betaChannel = catalogmetadata.Channel{Channel: declcfg.Channel{
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
		}}

		stableChannel = catalogmetadata.Channel{Channel: declcfg.Channel{
			Name: "beta",
			Entries: []declcfg.ChannelEntry{
				{
					Name: "operatorhub/packageA/2.0.0",
				},
			},
		}}

		testBundleList = []*catalogmetadata.Bundle{
			{Bundle: declcfg.Bundle{
				Name:    "operatorhub/prometheus/0.37.0",
				Package: "prometheus",
				Image:   "quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"0.37.0"}`)},
					{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"monitoring.coreos.com","kind":"Alertmanager","version":"v1"}, {"group":"monitoring.coreos.com","kind":"Prometheus","version":"v1"}]`)},
				},
			}, InChannels: []*catalogmetadata.Channel{&betaChannel}},
			{Bundle: declcfg.Bundle{
				Name:    "operatorhub/prometheus/0.47.0",
				Package: "prometheus",
				Image:   "quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"0.47.0"}`)},
					{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"monitoring.coreos.com","kind":"Alertmanager","version":"v1"}, {"group":"monitoring.coreos.com","kind":"Prometheus","version":"v1alpha1"}]`)},
				},
			}, InChannels: []*catalogmetadata.Channel{&betaChannel}},
			{Bundle: declcfg.Bundle{
				Name:    "operatorhub/packageA/2.0.0",
				Package: "packageA",
				Image:   "foo.io/packageA/packageA:v2.0.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"packageA","version":"2.0.0"}`)},
					{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
				},
			}, InChannels: []*catalogmetadata.Channel{&stableChannel}},
		}
	})

	It("should produce RequiredPackage variables", func() {
		bundleDeployments := []rukpakv1alpha1.BundleDeployment{
			bundleDeployment("prometheus", "quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35"),
		}

		bdVariableSource := variablesources.NewBundleDeploymentVariableSource(bundleDeployments, testBundleList, &MockRequiredPackageSource{})
		variables, err := bdVariableSource.GetVariables(context.Background())
		Expect(err).ToNot(HaveOccurred())

		installedPackageVariable := filterVariables[*olmvariables.InstalledPackageVariable](variables)
		Expect(installedPackageVariable).To(HaveLen(1))
		Expect(installedPackageVariable).To(WithTransform(func(bvars []*olmvariables.InstalledPackageVariable) map[deppy.Identifier]int {
			out := map[deppy.Identifier]int{}
			for _, variable := range bvars {
				out[variable.Identifier()] = len(variable.Bundles())
			}
			return out
		}, Equal(map[deppy.Identifier]int{
			// Underlying `InstalledPackageVariableSource` returns current installed package
			// as a possible upgrade edge
			deppy.IdentifierFromString("installed package prometheus"): 2,
		})))
	})
	It("should return an error if the bundleDeployment image doesn't match any operator resource", func() {
		bundleDeployments := []rukpakv1alpha1.BundleDeployment{
			bundleDeployment("prometheus", "quay.io/operatorhubio/prometheus@sha256:nonexistent"),
		}

		bdVariableSource := variablesources.NewBundleDeploymentVariableSource(bundleDeployments, testBundleList, &MockRequiredPackageSource{})
		_, err := bdVariableSource.GetVariables(context.Background())
		Expect(err.Error()).To(Equal("bundleImage \"quay.io/operatorhubio/prometheus@sha256:nonexistent\" not found"))
	})
})
