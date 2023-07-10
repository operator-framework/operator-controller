package operatore2e

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

type BundleInfo struct {
	BundleDirectory string
	BundleImageRef  string
}

var (
	cfg             *rest.Config
	c               client.Client
	operatorCatalog *catalogd.Catalog
	operator        *operatorv1alpha1.Operator
	err             error
	ctx             context.Context
	bundleInfo      = []BundleInfo{
		{ // The bundle directories of the plain bundles whose image is to be created and pushed along with the image reference
			BundleImageRef:  "localhost/testdata/bundles/plain-v0/plain:v0.1.0",
			BundleDirectory: "plain.v0.1.0",
		},
		{
			BundleImageRef:  "localhost/testdata/bundles/plain-v0/plain:v0.1.1",
			BundleDirectory: "plain.v0.1.1",
		},
	}
)

const (
	plainBundlesPath = "../../testdata/bundles/plain-v0"
	kindServer       = "operator-controller-e2e"

	catalogPath       = "../../testdata/catalogs"
	catalogName       = "plainv0-test-catalog"
	catalogImageRef   = "localhost/testdata/catalogs/plainv0-test-catalog:test"
	fbcConfigFilePath = "config/catalog_config.yaml"
	fbcFileName       = "catalog.yaml"
	opmPath           = "../../bin/opm"

	operatorName     = "plainv0-test-operator"
	createPkgName    = "plain"
	createPkgVersion = "0.1.0"
	updatePkgName    = "plain"
	updatePkgVersion = "0.1.1"
	nameSpace        = "rukpak-system"
)

func TestOperatorFramework(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operator Framework E2E Suite")
}

var _ = BeforeSuite(func() {

	var err error
	cfg = ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()

	err = catalogd.AddToScheme(scheme)
	Expect(err).ToNot(HaveOccurred())

	err = operatorv1alpha1.AddToScheme(scheme)
	Expect(err).To(Not(HaveOccurred()))

	err = rukpakv1alpha1.AddToScheme(scheme)
	Expect(err).To(Not(HaveOccurred()))

	c, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).To(Not(HaveOccurred()))

	ctx = context.Background()

})

var _ = AfterSuite(func() {
	Eventually(func(g Gomega) {
		// Deleting the catalog object and checking if the deletion was successful
		err = deleteAndCheckCatalogDeleted(operatorCatalog)
		g.Expect(errors.IsNotFound(err)).To(BeTrue())
	}).Should(Succeed())
})

var _ = Describe("Operator Framework E2E", func() {
	When("Create and load plain+v0 bundle images onto a test environment", func() {
		It("Create bundle images", func() {
			By("By building required bundle images")
			for _, bundle := range bundleInfo {
				err = buildContainer(bundle.BundleImageRef, plainBundlesPath+"/"+bundle.BundleDirectory+"/Dockerfile", plainBundlesPath+"/"+bundle.BundleDirectory, GinkgoWriter)
				Expect(err).To(Not(HaveOccurred()))
			}
		})

		It("Load bundle images onto test environment", func() {
			By("Loading bundle images")
			var images []string
			for _, info := range bundleInfo {
				images = append(images, info.BundleImageRef)
			}
			err = pushLoadImages(GinkgoWriter, kindServer, images...)
			Expect(err).To(Not(HaveOccurred()))
		})

	})
	When("Create FBC and validate FBC", func() {
		It("Create a FBC", func() {
			By("Forming the FBC content from catalog_config.yaml")
			fbc, err := CreateFBC(fbcConfigFilePath)
			Expect(err).To(Not(HaveOccurred()))

			By("Writing the FBC content to catalog.yaml file")
			err = WriteFBC(*fbc, catalogPath+"/"+catalogName, fbcFileName)
			Expect(err).To(Not(HaveOccurred()))
		})
		It("Validate FBC", func() {
			By("By validating the FBC using opm validate")
			err = validateFBC(catalogPath + "/" + catalogName)
			Expect(err).To(Not(HaveOccurred()))
		})
	})
	When("Create docker file and FBC image and load the FBC image onto test environment", func() {
		It("Create the docker file", func() {
			By("By calling dockerfile create function")
			err = generateDockerFile(catalogPath, catalogName, catalogName+".Dockerfile")
			Expect(err).To(Not(HaveOccurred()))
		})
		It("Create FBC image", func() {
			By("By building FBC image")
			err = buildContainer(catalogImageRef, catalogPath+"/"+catalogName+".Dockerfile", catalogPath, GinkgoWriter)
			Expect(err).To(Not(HaveOccurred()))
		})
		It("Load FBC image onto test environment", func() {
			By("Loading FBC image")
			err = pushLoadImages(GinkgoWriter, kindServer, catalogImageRef)
			Expect(err).To(Not(HaveOccurred()))
		})
	})
	When("A catalog object is created from the FBC image and deployed", func() {
		It("Deploy catalog object with FBC image", func() {
			By("Creating an operator catalog object")
			operatorCatalog, err = createTestCatalog(ctx, catalogName, catalogImageRef)
			Expect(err).ToNot(HaveOccurred())

			By("Eventually checking if catalog unpacking is successful")
			Eventually(func(g Gomega) {
				err = checkCatalogUnpacked(operatorCatalog)
				g.Expect(err).ToNot(HaveOccurred())
			}, 10*time.Second, 1).Should(Succeed())

			By("Eventually checking if package is created")
			Eventually(func(g Gomega) {
				err = checkPackageCreated()
				g.Expect(err).ToNot(HaveOccurred())
			}, 10*time.Second, 1).Should(Succeed())

			By("Eventually checking if bundle metadata is created")
			Eventually(func(g Gomega) {
				err = checkBundleMetadataCreated()
				g.Expect(err).ToNot(HaveOccurred())
			}).Should(Succeed())

		})
	})
	When("An operator is installed from an operator catalog", func() {
		It("Create an operator object and install it", func() {
			By("By creating an operator object")
			operator, err = createOperator(ctx, operatorName, createPkgName, createPkgVersion)
			Expect(err).ToNot(HaveOccurred())

			By("Eventually reporting a successful resolution and bundle path")
			Eventually(func(g Gomega) {
				err = checkResolutionAndBundlePath(operator)
				g.Expect(err).ToNot(HaveOccurred())
			}, 10*time.Second, 1).Should(Succeed())

			By("Eventually installing the operator successfully")
			Eventually(func(g Gomega) {
				err = checkOperatorInstalled(operator, createPkgVersion)
				g.Expect(err).ToNot(HaveOccurred())
			}, 10*time.Second, 1).Should(Succeed())

			By("Eventually installing the package successfully")
			Eventually(func(g Gomega) {
				err = checkPackageInstalled(operator)
				g.Expect(err).ToNot(HaveOccurred())
			}, 10*time.Second, 1).Should(Succeed())

			By("Eventually verifying the presence of relevant manifest on cluster from the bundle")
			Eventually(func(g Gomega) {
				err = checkManifestPresence(createPkgName, createPkgVersion)
				g.Expect(err).ToNot(HaveOccurred())
			}).Should(Succeed())
		})
	})
	When("An operator is upgraded to a higher version", func() {
		It("Upgrade to an higher version of the operator", func() {
			operator, err = updateOperator(ctx, operatorName, updatePkgName, updatePkgVersion)
			Expect(err).ToNot(HaveOccurred())

			By("Eventually reporting a successful resolution and bundle path for the upgraded version")
			Eventually(func(g Gomega) {
				err = checkResolutionAndBundlePath(operator)
				g.Expect(err).ToNot(HaveOccurred())
			}, 10*time.Second, 1).Should(Succeed())

			By("Eventually upgrading the operator successfully")
			Eventually(func(g Gomega) {
				err = checkOperatorInstalled(operator, updatePkgVersion)
				g.Expect(err).ToNot(HaveOccurred())
			}, 10*time.Second, 1).Should(Succeed())

			By("Eventually upgrading the package successfully")
			Eventually(func(g Gomega) {
				err = checkPackageInstalled(operator)
				g.Expect(err).ToNot(HaveOccurred())
			}, 10*time.Second, 1).Should(Succeed())

			By("Eventually verifying the presence of relevant manifest on cluster from the bundle")
			Eventually(func(g Gomega) {
				err = checkManifestPresence(updatePkgName, updatePkgVersion)
				g.Expect(err).ToNot(HaveOccurred())
			}).Should(Succeed())
		})
	})
	When("An operator is deleted", func() {
		It("Delete and operator", func() {
			operator, err = deleteOperator(ctx, operatorName)
			Expect(err).ToNot(HaveOccurred())

			By("Eventually the operator should not exists")
			Eventually(func(g Gomega) {
				err = checkOperatorDeleted(operator)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		})
	})
})

func validateFBC(fbcDirPath string) error {
	cmd := exec.Command(opmPath, "validate", fbcDirPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("FBC validation failed: %s", output)
	}
	return nil
}

func buildContainer(tag, dockerfilePath, dockerContext string, w io.Writer) error {
	cmd := exec.Command("docker", "build", "-t", tag, "-f", dockerfilePath, dockerContext)
	cmd.Stderr = w
	cmd.Stdout = w
	err := cmd.Run()
	return err
}

func pushLoadImages(w io.Writer, kindServerName string, images ...string) error {
	for _, image := range images {
		cmd := exec.Command("kind", "load", "docker-image", image, "--name", kindServerName)
		cmd.Stderr = w
		cmd.Stdout = w
		err := cmd.Run()
		if err != nil {
			return err
		}
	}
	return nil
}

func checkConditionEquals(actualCond, expectedCond *metav1.Condition) error {
	if actualCond == nil {
		return fmt.Errorf("Expected condition %s to not be nil", expectedCond.Type)
	}
	if actualCond.Status != expectedCond.Status {
		return fmt.Errorf("Expected status: %s, but got: %s", expectedCond.Status, actualCond.Status)
	}
	if actualCond.Reason != expectedCond.Reason {
		return fmt.Errorf("Expected reason: %s but got: %s", expectedCond.Reason, actualCond.Reason)
	}
	if !strings.Contains(actualCond.Message, expectedCond.Message) {
		return fmt.Errorf("Expected message: %s but got: %s", expectedCond.Message, actualCond.Message)
	}
	return nil
}

func checkCatalogUnpacked(operatorCatalog *catalogd.Catalog) error {
	err = c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, operatorCatalog)
	if err != nil {
		return err
	}
	cond := apimeta.FindStatusCondition(operatorCatalog.Status.Conditions, catalogd.TypeUnpacked)
	expectedCond := &metav1.Condition{
		Type:    catalogd.TypeUnpacked,
		Status:  metav1.ConditionTrue,
		Reason:  catalogd.ReasonUnpackSuccessful,
		Message: "successfully unpacked the catalog image",
	}
	err = checkConditionEquals(cond, expectedCond)
	return err
}

func checkPackageCreated() error {
	pList := &catalogd.PackageList{}
	err = c.List(ctx, pList)
	if err != nil {
		return err
	}
	if len(pList.Items) == 0 {
		return fmt.Errorf("Package is not created")
	}
	return nil
}

func checkBundleMetadataCreated() error {
	bmList := &catalogd.BundleMetadataList{}
	err = c.List(ctx, bmList)
	if err != nil {
		return err
	}
	if len(bmList.Items) == 0 {
		return fmt.Errorf("Bundle metadata is not created")
	}
	return nil
}

func checkResolutionAndBundlePath(operator *operatorv1alpha1.Operator) error {
	err = c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)
	if err != nil {
		return err
	}
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
	expectedCond := &metav1.Condition{
		Type:    operatorv1alpha1.TypeResolved,
		Status:  metav1.ConditionTrue,
		Reason:  operatorv1alpha1.ReasonSuccess,
		Message: "resolved to",
	}
	err = checkConditionEquals(cond, expectedCond)
	if err != nil {
		return err
	}
	if operator.Status.ResolvedBundleResource == "" {
		return fmt.Errorf("Resoved Bundle Resource is not found")
	}
	return nil
}

func checkOperatorInstalled(operator *operatorv1alpha1.Operator, operatorVersion string) error {
	err = c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)
	if err != nil {
		return err
	}
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeInstalled)
	expectedCond := &metav1.Condition{
		Type:    operatorv1alpha1.TypeResolved,
		Status:  metav1.ConditionTrue,
		Reason:  operatorv1alpha1.ReasonSuccess,
		Message: "installed from",
	}
	err = checkConditionEquals(cond, expectedCond)
	if err != nil {
		return err
	}
	if operator.Status.InstalledBundleResource == "" {
		return fmt.Errorf("Installed Bundle Resource is not found")
	}
	if operator.Spec.Version != operatorVersion {
		return fmt.Errorf("Expected operator version: %s but got: %s", operator.Spec.Version, operatorVersion)
	}
	return nil
}

func checkPackageInstalled(operator *operatorv1alpha1.Operator) error {
	bd := rukpakv1alpha1.BundleDeployment{}
	err = c.Get(ctx, types.NamespacedName{Name: operator.Name}, &bd)
	if err != nil {
		return err
	}
	if len(bd.Status.Conditions) != 2 {
		return fmt.Errorf("Two conditions for successful unpack and successful installation are not populated")
	}
	if bd.Status.Conditions[0].Reason != "UnpackSuccessful" {
		return fmt.Errorf("Expected status condition reason for successful unpack is not populated")
	}
	if bd.Status.Conditions[1].Reason != "InstallationSucceeded" {
		return fmt.Errorf("Expected status condition reason for successful installation is not populated")
	}
	return nil
}

func checkManifestPresence(packageName, version string) error {
	resources, _ := collectKubernetesObjects(packageName, version)
	for _, resource := range resources {
		gvk := schema.GroupVersionKind{
			Group:   "",
			Version: resource.APIVersion,
			Kind:    resource.Kind,
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		err = c.Get(ctx, types.NamespacedName{Name: resource.Metadata.Name, Namespace: nameSpace}, obj)
		if err != nil {
			return err
		}
	}
	return nil
}

func checkOperatorDeleted(operator *operatorv1alpha1.Operator) error {
	err = c.Get(ctx, types.NamespacedName{Name: operator.Name}, &operatorv1alpha1.Operator{})
	return err
}

func deleteAndCheckCatalogDeleted(catalog *catalogd.Catalog) error {
	err = c.Delete(ctx, operatorCatalog)
	if err != nil {
		return err
	}
	err = c.Get(ctx, types.NamespacedName{Name: catalog.Name}, &catalogd.Catalog{})
	return err
}

func createTestCatalog(ctx context.Context, name, imageRef string) (*catalogd.Catalog, error) {
	catalog := &catalogd.Catalog{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: catalogd.CatalogSpec{
			Source: catalogd.CatalogSource{
				Type: catalogd.SourceTypeImage,
				Image: &catalogd.ImageSource{
					Ref: imageRef,
				},
			},
		},
	}

	err := c.Create(ctx, catalog)
	return catalog, err
}

func createOperator(ctx context.Context, operatorName, packageName, version string) (*operatorv1alpha1.Operator, error) {
	operator := &operatorv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name: operatorName,
		},
		Spec: operatorv1alpha1.OperatorSpec{
			PackageName: packageName,
			Version:     version,
		},
	}

	err := c.Create(ctx, operator)
	return operator, err
}

func updateOperator(ctx context.Context, operatorName, packageName, version string) (*operatorv1alpha1.Operator, error) {
	operator := &operatorv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name: operatorName,
		},
	}
	err := c.Get(ctx, types.NamespacedName{Name: operatorName}, operator)
	if err != nil {
		return nil, err
	}
	operator.Spec.PackageName = packageName
	operator.Spec.Version = version

	err = c.Update(ctx, operator)
	return operator, err
}

func deleteOperator(ctx context.Context, operatorName string) (*operatorv1alpha1.Operator, error) {
	err := c.Get(ctx, types.NamespacedName{Name: operatorName}, operator)
	if err != nil {
		return nil, err
	}

	err = c.Delete(ctx, operator)
	return operator, err
}
