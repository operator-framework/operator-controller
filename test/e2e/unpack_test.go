package e2e

import (
	"context"
	"io"
	"os"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
		job     batchv1.Job
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

			By("Making sure the catalog content is available via the http server")
			err = c.Get(ctx, types.NamespacedName{Name: catalog.Name}, catalog)
			Expect(err).ToNot(HaveOccurred())
			job = batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-svr-job",
					Namespace: defaultSystemNamespace,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:    "test-svr",
									Image:   "curlimages/curl",
									Command: []string{"sh", "-c", "curl --silent --show-error --location -o - " + catalog.Status.ContentURL},
								},
							},
							RestartPolicy: "Never",
						},
					},
				},
			}
			err = c.Create(ctx, &job)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() (bool, error) {
				err = c.Get(ctx, types.NamespacedName{Name: "test-svr-job", Namespace: defaultSystemNamespace}, &job)
				if err != nil {
					return false, err
				}
				return job.Status.CompletionTime != nil && job.Status.Succeeded == 1, err
			}).Should(BeTrue())
			pods := &corev1.PodList{}
			Eventually(func() (bool, error) {
				err := c.List(context.Background(), pods, client.MatchingLabels{"job-name": "test-svr-job"})
				if err != nil {
					return false, err
				}
				return len(pods.Items) == 1, nil
			}).Should(BeTrue())

			expectedFBC, err := os.ReadFile("../../testdata/catalogs/test-catalog/expected_all.json")
			Expect(err).To(Not(HaveOccurred()))
			// Get logs of the Pod
			pod := pods.Items[0]
			logReader, err := kubeClient.CoreV1().Pods(defaultSystemNamespace).GetLogs(pod.Name, &corev1.PodLogOptions{}).Stream(context.Background())
			Expect(err).To(Not(HaveOccurred()))
			actualFBC, err := io.ReadAll(logReader)
			Expect(err).To(Not(HaveOccurred()))
			Expect(cmp.Diff(expectedFBC, actualFBC)).To(BeEmpty())
		})
		AfterEach(func() {
			Expect(c.Delete(ctx, &job)).To(Succeed())
			Expect(c.Delete(ctx, catalog)).To(Succeed())
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: catalog.Name}, &catalogd.Catalog{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		})
	})
})
