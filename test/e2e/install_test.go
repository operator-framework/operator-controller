package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/utils/env"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

const (
	artifactName = "operator-controller-e2e"
)

var _ = Describe("Operator Install", func() {
	var (
		ctx          context.Context
		operatorName string
		operator     *operatorv1alpha1.Operator
	)
	When("An operator is installed from an operator catalog", func() {
		BeforeEach(func() {
			ctx = context.Background()
			operatorName = fmt.Sprintf("operator-%s", rand.String(8))
			operator = &operatorv1alpha1.Operator{
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorName,
				},
			}
		})
		When("the operator bundle format is registry+v1", func() {
			BeforeEach(func() {
				operator.Spec = operatorv1alpha1.OperatorSpec{
					PackageName: "prometheus",
					Version:     "0.47.0",
				}
			})
			It("resolves the specified package with correct bundle path", func() {
				By("creating the Operator resource")
				Expect(c.Create(ctx, operator)).To(Succeed())

				By("eventually reporting a successful resolution and bundle path")
				Eventually(func(g Gomega) {
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
					g.Expect(operator.Status.Conditions).To(HaveLen(2))
					cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
					g.Expect(cond.Message).To(ContainSubstring("resolved to"))
					g.Expect(operator.Status.ResolvedBundleResource).ToNot(BeEmpty())
				}).Should(Succeed())

				By("eventually installing the package successfully")
				Eventually(func(g Gomega) {
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
					cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeInstalled)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
					g.Expect(cond.Message).To(ContainSubstring("installed from"))
					g.Expect(operator.Status.InstalledBundleResource).ToNot(BeEmpty())

					bd := rukpakv1alpha1.BundleDeployment{}
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operatorName}, &bd)).To(Succeed())
					g.Expect(bd.Status.Conditions).To(HaveLen(2))
					g.Expect(bd.Status.Conditions[0].Reason).To(Equal("UnpackSuccessful"))
					g.Expect(bd.Status.Conditions[1].Reason).To(Equal("InstallationSucceeded"))
				}).Should(Succeed())
			})
		})

		When("the operator bundle format is plain+v0", func() {
			BeforeEach(func() {
				operator.Spec = operatorv1alpha1.OperatorSpec{
					PackageName: "plain",
				}
			})
			It("resolves the specified package with correct bundle path", func() {
				By("creating the Operator resource")
				Expect(c.Create(ctx, operator)).To(Succeed())

				By("eventually reporting a successful resolution and bundle path")
				Eventually(func(g Gomega) {
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
					g.Expect(operator.Status.Conditions).To(HaveLen(2))
					cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
					g.Expect(cond.Message).To(ContainSubstring("resolved to"))
					g.Expect(operator.Status.ResolvedBundleResource).ToNot(BeEmpty())
				}).Should(Succeed())

				By("eventually installing the package successfully")
				Eventually(func(g Gomega) {
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
					cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeInstalled)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
					g.Expect(cond.Message).To(ContainSubstring("installed from"))
					g.Expect(operator.Status.InstalledBundleResource).ToNot(BeEmpty())

					bd := rukpakv1alpha1.BundleDeployment{}
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operatorName}, &bd)).To(Succeed())
					g.Expect(bd.Status.Conditions).To(HaveLen(2))
					g.Expect(bd.Status.Conditions[0].Reason).To(Equal("UnpackSuccessful"))
					g.Expect(bd.Status.Conditions[1].Reason).To(Equal("InstallationSucceeded"))
				}).Should(Succeed())
			})
		})

		It("resolves again when a new catalog is available", func() {
			pkgName := "prometheus"
			operator.Spec = operatorv1alpha1.OperatorSpec{
				PackageName: pkgName,
			}

			// Delete the catalog first
			Expect(c.Delete(ctx, operatorCatalog)).To(Succeed())

			Eventually(func(g Gomega) {
				// target package should not be present on cluster
				err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s-%s", operatorCatalog.Name, declcfg.SchemaPackage, pkgName)}, &catalogd.CatalogMetadata{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())

			By("creating the Operator resource")
			Expect(c.Create(ctx, operator)).To(Succeed())

			By("failing to find Operator during resolution")
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonResolutionFailed))
				g.Expect(cond.Message).To(Equal(fmt.Sprintf("package '%s' not found", pkgName)))
			}).Should(Succeed())

			By("creating an Operator catalog with the desired package")
			var err error
			operatorCatalog, err = createTestCatalog(ctx, testCatalogName, getCatalogImageRef())
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, operatorCatalog)).To(Succeed())
				cond := apimeta.FindStatusCondition(operatorCatalog.Status.Conditions, catalogd.TypeUnpacked)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(catalogd.ReasonUnpackSuccessful))
			}).Should(Succeed())

			By("eventually resolving the package successfully")
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
			}).Should(Succeed())
		})

		It("handles upgrade edges correctly", func() {
			By("creating a valid Operator resource")
			operator.Spec = operatorv1alpha1.OperatorSpec{
				PackageName: "prometheus",
				Version:     "0.37.0",
			}
			Expect(c.Create(ctx, operator)).To(Succeed())
			By("eventually reporting a successful resolution")
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
				g.Expect(cond.Message).To(ContainSubstring("resolved to"))
				g.Expect(operator.Status.ResolvedBundleResource).ToNot(BeEmpty())
			}).Should(Succeed())

			By("updating the Operator resource to a non-successor version")
			operator.Spec.Version = "0.65.1" // current (0.37.0) and successor (0.47.0) are the only values that would be SAT.
			Expect(c.Update(ctx, operator)).To(Succeed())
			By("eventually reporting an unsatisfiable resolution")
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonResolutionFailed))
				g.Expect(cond.Message).To(MatchRegexp(`^constraints not satisfiable:.*; installed package prometheus requires at least one of.*0.47.0[^,]*,[^,]*0.37.0[^;]*;.*`))
				g.Expect(operator.Status.ResolvedBundleResource).To(BeEmpty())
			}).Should(Succeed())

			By("updating the Operator resource to a valid upgrade edge")
			operator.Spec.Version = "0.47.0"
			Expect(c.Update(ctx, operator)).To(Succeed())
			By("eventually reporting a successful resolution and bundle path")
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
				g.Expect(cond.Message).To(ContainSubstring("resolved to"))
				g.Expect(operator.Status.ResolvedBundleResource).ToNot(BeEmpty())
			}).Should(Succeed())
		})

		AfterEach(func() {
			if basePath := env.GetString("ARTIFACT_PATH", ""); basePath != "" {
				// get all the artifacts from the test run and save them to the artifact path
				getArtifactsOutput(ctx, basePath)
			}
			Expect(c.Delete(ctx, operator)).To(Succeed())
			Eventually(func(g Gomega) {
				err := c.Get(ctx, types.NamespacedName{Name: operator.Name}, &operatorv1alpha1.Operator{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		})

	})
})

// getArtifactsOutput gets all the artifacts from the test run and saves them to the artifact path.
// Currently it saves:
// - operators
// - pods logs
// - deployments
// - bundle
// - bundledeployments
// - catalogsources
func getArtifactsOutput(ctx context.Context, basePath string) {
	kubeClient, err := kubeclient.NewForConfig(cfg)
	Expect(err).To(Not(HaveOccurred()))

	// sanitize the artifact name for use as a directory name
	testName := strings.ReplaceAll(strings.ToLower(CurrentSpecReport().LeafNodeText), " ", "-")
	// Get the test description and sanitize it for use as a directory name
	artifactPath := filepath.Join(basePath, artifactName, fmt.Sprint(time.Now().UnixNano()), testName)

	// Create the full artifact path
	err = os.MkdirAll(artifactPath, 0755)
	Expect(err).To(Not(HaveOccurred()))

	// Get all namespaces
	namespaces := corev1.NamespaceList{}
	if err := c.List(ctx, &namespaces); err != nil {
		GinkgoWriter.Printf("Failed to list namespaces %w", err)
	}

	// get all operators save them to the artifact path.
	operators := operatorv1alpha1.OperatorList{}
	if err := c.List(ctx, &operators, client.InNamespace("")); err != nil {
		GinkgoWriter.Printf("Failed to list operators %w", err)
	}
	for _, operator := range operators.Items {
		// Save operator to artifact path
		operatorYaml, err := yaml.Marshal(operator)
		if err != nil {
			GinkgoWriter.Printf("Failed to marshal operator %w", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, operator.Name+"-operator.yaml"), operatorYaml, 0600); err != nil {
			GinkgoWriter.Printf("Failed to write operator to file %w", err)
		}
	}

	// get all catalogsources save them to the artifact path.
	catalogsources := catalogd.CatalogList{}
	if err := c.List(ctx, &catalogsources, client.InNamespace("")); err != nil {
		GinkgoWriter.Printf("Failed to list catalogsources %w", err)
	}
	for _, catalogsource := range catalogsources.Items {
		// Save catalogsource to artifact path
		catalogsourceYaml, err := yaml.Marshal(catalogsource)
		if err != nil {
			GinkgoWriter.Printf("Failed to marshal catalogsource %w", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, catalogsource.Name+"-catalogsource.yaml"), catalogsourceYaml, 0600); err != nil {
			GinkgoWriter.Printf("Failed to write catalogsource to file %w", err)
		}
	}

	// Get all Bundles in the namespace and save them to the artifact path.
	bundles := rukpakv1alpha1.BundleList{}
	if err := c.List(ctx, &bundles, client.InNamespace("")); err != nil {
		GinkgoWriter.Printf("Failed to list bundles %w", err)
	}
	for _, bundle := range bundles.Items {
		// Save bundle to artifact path
		bundleYaml, err := yaml.Marshal(bundle)
		if err != nil {
			GinkgoWriter.Printf("Failed to marshal bundle %w", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, bundle.Name+"-bundle.yaml"), bundleYaml, 0600); err != nil {
			GinkgoWriter.Printf("Failed to write bundle to file %w", err)
		}
	}

	// Get all BundleDeployments in the namespace and save them to the artifact path.
	bundleDeployments := rukpakv1alpha1.BundleDeploymentList{}
	if err := c.List(ctx, &bundleDeployments, client.InNamespace("")); err != nil {
		GinkgoWriter.Printf("Failed to list bundleDeployments %w", err)
	}
	for _, bundleDeployment := range bundleDeployments.Items {
		// Save bundleDeployment to artifact path
		bundleDeploymentYaml, err := yaml.Marshal(bundleDeployment)
		if err != nil {
			GinkgoWriter.Printf("Failed to marshal bundleDeployment %w", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, bundleDeployment.Name+"-bundleDeployment.yaml"), bundleDeploymentYaml, 0600); err != nil {
			GinkgoWriter.Printf("Failed to write bundleDeployment to file %w", err)
		}
	}

	for _, namespace := range namespaces.Items {
		// let's ignore kube-* namespaces.
		if strings.Contains(namespace.Name, "kube-") {
			continue
		}

		namespacedArtifactPath := filepath.Join(artifactPath, namespace.Name)
		if err := os.Mkdir(namespacedArtifactPath, 0755); err != nil {
			GinkgoWriter.Printf("Failed to create namespaced artifact path %w", err)
			continue
		}

		// get all deployments in the namespace and save them to the artifact path.
		deployments := appsv1.DeploymentList{}
		if err := c.List(ctx, &deployments, client.InNamespace(namespace.Name)); err != nil {
			GinkgoWriter.Printf("Failed to list deployments %w in namespace: %q", err, namespace.Name)
			continue
		}

		for _, deployment := range deployments.Items {
			// Save deployment to artifact path
			deploymentYaml, err := yaml.Marshal(deployment)
			if err != nil {
				GinkgoWriter.Printf("Failed to marshal deployment %w", err)
				continue
			}
			if err := os.WriteFile(filepath.Join(namespacedArtifactPath, deployment.Name+"-deployment.yaml"), deploymentYaml, 0600); err != nil {
				GinkgoWriter.Printf("Failed to write deployment to file %w", err)
			}
		}

		// Get logs from all pods in all namespaces
		pods := corev1.PodList{}
		if err := c.List(ctx, &pods, client.InNamespace(namespace.Name)); err != nil {
			GinkgoWriter.Printf("Failed to list pods %w in namespace: %q", err, namespace.Name)
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
				continue
			}
			for _, container := range pod.Spec.Containers {
				logs, err := kubeClient.CoreV1().Pods(namespace.Name).GetLogs(pod.Name, &corev1.PodLogOptions{Container: container.Name}).Stream(ctx)
				if err != nil {
					GinkgoWriter.Printf("Failed to get logs for pod %q in namespace %q: %w", pod.Name, namespace.Name, err)
					continue
				}
				defer logs.Close()

				outFile, err := os.Create(filepath.Join(namespacedArtifactPath, pod.Name+"-"+container.Name+"-logs.txt"))
				if err != nil {
					GinkgoWriter.Printf("Failed to create file for pod %q in namespace %q: %w", pod.Name, namespace.Name, err)
					continue
				}
				defer outFile.Close()

				if _, err := io.Copy(outFile, logs); err != nil {
					GinkgoWriter.Printf("Failed to copy logs for pod %q in namespace %q: %w", pod.Name, namespace.Name, err)
					continue
				}
			}
		}
	}
}
