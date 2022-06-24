package e2e

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deppyv1alpha1 "github.com/operator-framework/deppy/api/v1alpha1"
)

var _ = FDescribe("catalog source deppy adapter", func() {
	var (
		ns      *corev1.Namespace
		ctx     context.Context
		catalog MagicCatalog
	)
	BeforeEach(func() {
		ctx = context.Background()
		ns = SetupTestNamespace(c, genName("e2e-"))

		provider, err := NewFileBasedFiledBasedCatalogProvider(filepath.Join(dataBaseDir, "prometheus.v0.1.0.yaml"))
		Expect(err).To(BeNil())

		catalog = NewMagicCatalog(c, ns.GetName(), "prometheus", provider)
		Expect(catalog.DeployCatalog(ctx)).To(BeNil())
	})
	AfterEach(func() {
		Expect(c.Delete(ctx, ns)).To(BeNil())
		Expect(catalog.UndeployCatalog(ctx)).To(BeNil())

		// Note: hack around the CS adapter implementation that doesn't properly cleanup
		// the generated set of Input resources
		inputs := &deppyv1alpha1.InputList{}
		Expect(c.List(ctx, inputs, &client.ListOptions{LabelSelector: newCatalogLabelSelector([]string{"prometheus"})})).To(BeNil())
		for _, input := range inputs.Items {
			input := input
			Expect(c.Delete(ctx, &input)).To(BeNil())
		}
	})
	When("a catalog source has been created", func() {
		It("should create inputs for each package in the catalog", func() {
			Eventually(func() bool {
				inputs := &deppyv1alpha1.InputList{}
				if err := c.List(ctx, inputs, &client.ListOptions{
					LabelSelector: newCatalogLabelSelector([]string{"prometheus"}),
				}); err != nil {
					return false
				}
				// Note: there's a single olm.bundle defined in that testdata FBC
				return len(inputs.Items) == 1
			}).Should(BeTrue())
		})
		It("should generate an Input with the correct properties", func() {
			Eventually(func() bool {
				inputs := &deppyv1alpha1.InputList{}
				if err := c.List(ctx, inputs, &client.ListOptions{
					LabelSelector: newCatalogLabelSelector([]string{"prometheus"}),
				}); err != nil {
					return false
				}
				if len(inputs.Items) != 1 {
					return false
				}
				input := inputs.Items[0]
				if len(input.Spec.Properties) != 1 {
					return false
				}
				property := input.Spec.Properties[0]
				if property.Type != "olm.package" {
					return false
				}
				return property.Value["package"] == "prometheus-operator"
			}).Should(BeTrue())
		})
	})
	// Note: skip this test for now until we come up with a better implementations,
	// or refactor the adapter implementation to avoid creating Input resources through
	// a Kubernetes API.
	PWhen("a catalog source has been deleted", func() {
		It("should delete an inputs that were sourced from that individual catalog", func() {
			Eventually(func() bool {
				inputs := &deppyv1alpha1.InputList{}
				if err := c.List(ctx, inputs, &client.ListOptions{
					LabelSelector: newCatalogLabelSelector([]string{"prometheus"}),
				}); err != nil {
					return false
				}
				return len(inputs.Items) == 0
			}).Should(BeTrue())
		})
	})
})

func newCatalogLabelSelector(values []string) labels.Selector {
	selector, err := labels.NewRequirement("deppy.adapter.catalog/source-name", selection.Equals, values)
	if err != nil {
		panic(err)
	}
	return labels.NewSelector().Add(*selector)
}
