package e2e

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

const (
	defaultTimeout = 30 * time.Second
	defaultPoll    = 1 * time.Second
)

var _ = Describe("Operator Install", func() {
	var (
		ctx          context.Context
		pkgName      string
		operatorName string
		operator     *operatorv1alpha1.Operator
	)

	var testNamespace string
	cleanup := func() {}
	ctx = context.TODO()
	BeforeEach(func() {
		testNamespace := createTestNamespace(ctx, c, "registry-grpc-")
		cleanup = applyCRDifNotPresent(ctx)
		testPrefix := "registry-grpc-"

		serviceAccountName := createTestServiceAccount(ctx, c, testNamespace, testPrefix)
		createTestRegistryPod(ctx, c, testNamespace, testPrefix, serviceAccountName)
		serviceName := createTestRegistryService(ctx, c, testNamespace, testPrefix)
		createTestCatalogSource(ctx, c, testNamespace, "prometheus-index", serviceName)

	})
	AfterEach(func() {
		deleteTestNamespace(ctx, c, testNamespace)
		cleanup()
	})
	It("resolves the specified package with correct bundle path", func() {
		ctx = context.Background()
		pkgName = "prometheus"
		operatorName = fmt.Sprintf("operator-%s", rand.String(8))
		operator = &operatorv1alpha1.Operator{
			ObjectMeta: metav1.ObjectMeta{
				Name: operatorName,
			},
			Spec: operatorv1alpha1.OperatorSpec{
				PackageName: pkgName,
			},
		}

		By("creating the Operator resource")
		err := c.Create(ctx, operator)
		Expect(err).ToNot(HaveOccurred())

		// TODO dfranz: This test currently relies on the hard-coded CatalogSources found in bundle_cache.go
		// and should be re-worked to use a real or test catalog source when the hard-coded stuff is removed
		By("eventually reporting a successful resolution and bundle path")
		Eventually(func(g Gomega) {
			err = c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(len(operator.Status.Conditions)).To(Equal(1))
			g.Expect(operator.Status.Conditions[0].Message).To(Equal("install was successful"))
		}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())

		By("eventually installing the package successfully")
		Eventually(func(g Gomega) {
			bd := rukpakv1alpha1.BundleDeployment{}
			err = c.Get(ctx, types.NamespacedName{Name: operatorName}, &bd)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(len(bd.Status.Conditions)).To(Equal(2))
			g.Expect(bd.Status.Conditions[0].Reason).To(Equal("UnpackSuccessful"))
			g.Expect(bd.Status.Conditions[1].Reason).To(Equal("InstallationSucceeded"))
		}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())

		By("deleting the Operator resource")
		err = c.Delete(ctx, operator)
		Expect(err).ToNot(HaveOccurred())
	})
})
