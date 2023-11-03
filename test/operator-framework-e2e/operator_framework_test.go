package operatore2e

import (
	"context"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var (
	cfg *rest.Config
	c   client.Client
	ctx context.Context
)

func TestOperatorFramework(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operator Framework E2E Suite")
}

var _ = BeforeSuite(func() {
	cfg = ctrl.GetConfigOrDie()
	scheme := runtime.NewScheme()
	ctx = context.Background()

	err := catalogd.AddToScheme(scheme)
	Expect(err).ToNot(HaveOccurred())

	err = operatorv1alpha1.AddToScheme(scheme)
	Expect(err).ToNot(HaveOccurred())

	c, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())
})

var _ = Describe("Operator Framework E2E", func() {
	var catalog *catalogd.Catalog
	BeforeEach(func() {
		catalog = &catalogd.Catalog{
			ObjectMeta: metav1.ObjectMeta{
				Name: "catalog" + rand.String(10),
			},
			Spec: catalogd.CatalogSpec{
				Source: catalogd.CatalogSource{
					Type: catalogd.SourceTypeImage,
					Image: &catalogd.ImageSource{
						Ref:                   os.Getenv("CATALOG_IMG"),
						InsecureSkipTLSVerify: true,
					},
				},
			},
		}
		Expect(c.Create(ctx, catalog)).NotTo(HaveOccurred())
	})
	When("Creating an Operator that references a package with a plain+v0 bundle type", func() {
		var operator *operatorv1alpha1.Operator
		BeforeEach(func() {
			operator = &operatorv1alpha1.Operator{
				ObjectMeta: metav1.ObjectMeta{
					Name: "plainv0",
				},
				Spec: operatorv1alpha1.OperatorSpec{
					PackageName: os.Getenv("PLAIN_PKG_NAME"),
				},
			}

			Expect(c.Create(ctx, operator)).NotTo(HaveOccurred())
		})
		It("should have a status condition type of Installed with a status of True and a reason of Success", func() {
			Eventually(func(g Gomega) {
				op := &operatorv1alpha1.Operator{}
				g.Expect(c.Get(ctx, client.ObjectKeyFromObject(operator), op)).NotTo(HaveOccurred())
				cond := meta.FindStatusCondition(op.Status.Conditions, operatorv1alpha1.TypeInstalled)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).Should(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).Should(Equal(operatorv1alpha1.ReasonSuccess))
			}).WithTimeout(2 * time.Minute).Should(Succeed())
		})
		AfterEach(func() {
			Expect(c.Delete(ctx, operator)).NotTo(HaveOccurred())
		})
	})
	When("Creating an Operator that references a package with a registry+v1 bundle type", func() {
		var operator *operatorv1alpha1.Operator
		BeforeEach(func() {
			operator = &operatorv1alpha1.Operator{
				ObjectMeta: metav1.ObjectMeta{
					Name: "registryv1",
				},
				Spec: operatorv1alpha1.OperatorSpec{
					PackageName: os.Getenv("REG_PKG_NAME"),
				},
			}

			Expect(c.Create(ctx, operator)).NotTo(HaveOccurred())
		})
		It("should have a status condition type of Installed with a status of True and a reason of Success", func() {
			Eventually(func(g Gomega) {
				op := &operatorv1alpha1.Operator{}
				g.Expect(c.Get(ctx, client.ObjectKeyFromObject(operator), op)).NotTo(HaveOccurred())
				cond := meta.FindStatusCondition(op.Status.Conditions, operatorv1alpha1.TypeInstalled)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).Should(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).Should(Equal(operatorv1alpha1.ReasonSuccess))
			}).WithTimeout(2 * time.Minute).Should(Succeed())
		})
		AfterEach(func() {
			Expect(c.Delete(ctx, operator)).NotTo(HaveOccurred())
		})
	})

	AfterEach(func() {
		Expect(c.Delete(ctx, catalog)).NotTo(HaveOccurred())
	})
})
