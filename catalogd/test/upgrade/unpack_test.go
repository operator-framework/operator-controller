package upgradee2e

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogdv1 "github.com/operator-framework/catalogd/api/v1"
	"github.com/operator-framework/catalogd/test/e2e"
)

var _ = Describe("ClusterCatalog Unpacking", func() {
	When("A ClusterCatalog is created", func() {
		It("Successfully unpacks catalog contents", func() {
			ctx := context.Background()

			var managerDeployment appsv1.Deployment
			managerLabelSelector := labels.Set{"control-plane": "catalogd-controller-manager"}
			By("Checking that the controller-manager deployment is updated")
			Eventually(func(g Gomega) {
				var managerDeployments appsv1.DeploymentList
				err := c.List(ctx, &managerDeployments, client.MatchingLabels(managerLabelSelector), client.InNamespace("olmv1-system"))
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(managerDeployments.Items).To(HaveLen(1))
				managerDeployment = managerDeployments.Items[0]
				g.Expect(managerDeployment.Status.UpdatedReplicas).To(Equal(*managerDeployment.Spec.Replicas))
				g.Expect(managerDeployment.Status.Replicas).To(Equal(*managerDeployment.Spec.Replicas))
				g.Expect(managerDeployment.Status.AvailableReplicas).To(Equal(*managerDeployment.Spec.Replicas))
				g.Expect(managerDeployment.Status.ReadyReplicas).To(Equal(*managerDeployment.Spec.Replicas))
			}).Should(Succeed())

			var managerPod corev1.Pod
			By("Waiting for only one controller-manager pod to remain")
			Eventually(func(g Gomega) {
				var managerPods corev1.PodList
				err := c.List(ctx, &managerPods, client.MatchingLabels(managerLabelSelector))
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(managerPods.Items).To(HaveLen(1))
				managerPod = managerPods.Items[0]
			}).Should(Succeed())

			By("Reading logs to make sure that ClusterCatalog was reconciled by catalogdv1")
			logCtx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()
			substrings := []string{
				"reconcile ending",
				fmt.Sprintf(`ClusterCatalog=%q`, testClusterCatalogName),
			}
			found, err := watchPodLogsForSubstring(logCtx, &managerPod, "manager", substrings...)
			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeTrue())

			catalog := &catalogdv1.ClusterCatalog{}
			By("Ensuring ClusterCatalog has Status.Condition of Progressing with a status == True, reason == Succeeded")
			Eventually(func(g Gomega) {
				err := c.Get(ctx, types.NamespacedName{Name: testClusterCatalogName}, catalog)
				g.Expect(err).ToNot(HaveOccurred())
				cond := meta.FindStatusCondition(catalog.Status.Conditions, catalogdv1.TypeProgressing)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(catalogdv1.ReasonSucceeded))
			}).Should(Succeed())

			expectedFBC, err := os.ReadFile("../../testdata/catalogs/test-catalog/expected_all.json")
			Expect(err).To(Not(HaveOccurred()))

			By("Making sure the catalog content is available via the http server")
			Eventually(func(g Gomega) {
				actualFBC, err := e2e.ReadTestCatalogServerContents(ctx, catalog, c, kubeClient)
				g.Expect(err).To(Not(HaveOccurred()))
				g.Expect(cmp.Diff(expectedFBC, actualFBC)).To(BeEmpty())
			}).Should(Succeed())

			By("Ensuring ClusterCatalog has Status.Condition of Serving with a status == True, reason == Available")
			Eventually(func(g Gomega) {
				err := c.Get(ctx, types.NamespacedName{Name: testClusterCatalogName}, catalog)
				g.Expect(err).ToNot(HaveOccurred())
				cond := meta.FindStatusCondition(catalog.Status.Conditions, catalogdv1.TypeServing)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(catalogdv1.ReasonAvailable))
			}).Should(Succeed())
		})
	})
})

func watchPodLogsForSubstring(ctx context.Context, pod *corev1.Pod, container string, substrings ...string) (bool, error) {
	podLogOpts := corev1.PodLogOptions{
		Follow:    true,
		Container: container,
	}

	req := kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return false, err
	}
	defer podLogs.Close()

	scanner := bufio.NewScanner(podLogs)
	for scanner.Scan() {
		line := scanner.Text()

		foundCount := 0
		for _, substring := range substrings {
			if strings.Contains(line, substring) {
				foundCount++
			}
		}
		if foundCount == len(substrings) {
			return true, nil
		}
	}

	return false, scanner.Err()
}
