package e2e

import (
	"context"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
// variable TEST_CATALOG_IMAGE if set, falling back to docker-registry.catalogd-e2e.svc:5000/test-catalog:e2e otherwise.
func catalogImageRef() string {
	if s := os.Getenv(catalogRefEnvVar); s != "" {
		return s
	}

	return "docker-registry.catalogd-e2e.svc:5000/test-catalog:e2e"
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
							Ref:                   catalogImageRef(),
							InsecureSkipTLSVerify: true,
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

			By("Making sure the catalog content is available via the http server")
			err = c.Get(ctx, types.NamespacedName{Name: catalog.Name}, catalog)
			url, err := url.Parse(catalog.Status.ContentURL)
			Expect(err).NotTo(HaveOccurred())
			Expect(url).NotTo(BeNil())
			// url is expected to be in the format of
			// http://{service_name}.{namespace}.svc/{catalog_name}/all.json
			// so to get the namespace and name of the service we grab only
			// the hostname and split it on the '.' character
			ns := strings.Split(url.Hostname(), ".")[1]
			name := strings.Split(url.Hostname(), ".")[0]
			port := url.Port()
			// the ProxyGet() call below needs an explicit port value, so if
			// value from url.Port() is empty, we assume port 80.
			if port == "" {
				port = "80"
			}
			resp := kubeClient.CoreV1().Services(ns).ProxyGet(url.Scheme, name, port, url.Path, map[string]string{})
			rc, err := resp.Stream(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer rc.Close()

			expectedFBC, err := os.ReadFile("../../testdata/catalogs/test-catalog/expected_all.json")
			Expect(err).To(Not(HaveOccurred()))

			actualFBC, err := io.ReadAll(rc)
			Expect(err).To(Not(HaveOccurred()))
			Expect(cmp.Diff(expectedFBC, actualFBC)).To(BeEmpty())
		})
		AfterEach(func() {
			Expect(c.Delete(ctx, catalog)).To(Succeed())
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: catalog.Name}, &catalogd.Catalog{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		})
	})
})
