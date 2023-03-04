package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("catalog controller test", func() {
	var (
		ctx context.Context
	)

	var testPrefix = "registry-grpc-"
	var catsrcName = "prometheus-index"

	When("testing if catalogsrc reconciles successfully", func() {
		ctx = context.Background()
		testNamespace := createTestNamespace(ctx, c, testPrefix)

		serviceAccountName := createTestServiceAccount(ctx, c, testNamespace, testPrefix)
		createTestRegistryPod(ctx, c, testNamespace, testPrefix, serviceAccountName)

		serviceName := createTestRegistryService(ctx, c, testNamespace, testPrefix)
		createTestCatalogSource(ctx, c, testNamespace, catsrcName, serviceName)

		// check if you are able to fetch catsrc
		catsrc := &v1alpha1.CatalogSource{}
		// put this in the helper itself.
		Eventually(func() {
			err := c.Get(ctx, types.NamespacedName{Name: catsrcName}, catsrc)
			Expect(err).NotTo(HaveOccurred())
		})

		// How do we test the entity cache?

	})
})
