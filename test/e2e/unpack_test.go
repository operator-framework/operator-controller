package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
)

const (
	catalogRefEnvVar = "TEST_CATALOG_IMAGE"
	catalogName      = "test-catalog"
	pkg              = "prometheus"
	version          = "0.47.0"
	channel          = "beta"
	bundle           = "prometheus-operator.0.47.0"
	bundleImage      = "localhost/testdata/bundles/registry-v1/prometheus-operator:v0.47.0"
)

// catalogImageRef returns the image reference for the test catalog image, defaulting to the value of the environment
// variable TEST_CATALOG_IMAGE if set, falling back to localhost/testdata/catalogs/test-catalog:e2e otherwise.
func catalogImageRef() string {
	if s := os.Getenv(catalogRefEnvVar); s != "" {
		return s
	}

	return "localhost/testdata/catalogs/test-catalog:e2e"
}

var _ = Describe("Catalog Unpacking", func() {
	var (
		ctx     context.Context
		catalog *catalogd.Catalog
	)
	When("A Catalog is created", func() {
		BeforeEach(func() {
			ctx = context.Background()
			var err error

			catalog = &catalogd.Catalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogName,
				},
				Spec: catalogd.CatalogSpec{
					Source: catalogd.CatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ImageSource{
							Ref: catalogImageRef(),
						},
					},
				},
			}

			err = c.Create(ctx, catalog)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Successfully unpacks catalog contents", func() {
			By("Ensuring Catalog has Status.Condition of Unpacked with a status == True")
			Eventually(func(g Gomega) {
				err := c.Get(ctx, types.NamespacedName{Name: catalog.Name}, catalog)
				g.Expect(err).ToNot(HaveOccurred())
				cond := meta.FindStatusCondition(catalog.Status.Conditions, catalogd.TypeUnpacked)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(catalogd.ReasonUnpackSuccessful))
			}).Should(Succeed())

			By("Ensuring the expected Package resource is created")
			pack := &catalogd.Package{}
			expectedPackSpec := catalogd.PackageSpec{
				Catalog: corev1.LocalObjectReference{
					Name: catalogName,
				},
				Channels: []catalogd.PackageChannel{
					{
						Name: channel,
						Entries: []catalogd.ChannelEntry{
							{
								Name: bundle,
							},
						},
					},
				},
				DefaultChannel: channel,
				Name:           pkg,
			}
			err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s", catalog.Name, pkg)}, pack)
			Expect(err).ToNot(HaveOccurred())
			Expect(pack.Spec).To(Equal(expectedPackSpec))

			By("Ensuring the expected BundleMetadata resource is created")
			bm := &catalogd.BundleMetadata{}
			expectedBMSpec := catalogd.BundleMetadataSpec{
				Catalog: corev1.LocalObjectReference{
					Name: catalogName,
				},
				Package: pkg,
				Image:   bundleImage,
				Properties: []catalogd.Property{
					{
						Type:  "olm.package",
						Value: json.RawMessage(`{"packageName":"prometheus","version":"0.47.0"}`),
					},
				},
			}
			err = c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s", catalog.Name, bundle)}, bm)
			Expect(err).ToNot(HaveOccurred())
			Expect(bm.Spec).To(Equal(expectedBMSpec))
		})

		AfterEach(func() {
			err := c.Delete(ctx, catalog)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: catalog.Name}, &catalogd.Catalog{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		})
	})
})
