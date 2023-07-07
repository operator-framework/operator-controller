package operatore2e

import (
	"context"
	"fmt"
	"io"
	"os/exec"
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
		{
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
	Expect(c.Delete(ctx, operatorCatalog)).To(Succeed())
	Eventually(func(g Gomega) {
		err := c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, &catalogd.Catalog{})
		Expect(errors.IsNotFound(err)).To(BeTrue())
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
			By("Creating a operator catalog object")
			operatorCatalog, err = createTestCatalog(ctx, catalogName, catalogImageRef)
			Expect(err).ToNot(HaveOccurred())

			By("Eventually checking if catalog unpacking is successful")
			checkCatalogUnpacked(operatorCatalog)

			By("Eventually checking if package is created")
			checkPackageCreated()

			By("Eventually checking if bundle metadata is created")
			checkBundleMetadataCreated()

		})
	})
	When("An operator is installed from an operator catalog", func() {
		It("Create an operator object and install it", func() {
			By("By creating an operator object")
			operator, err = createOperator(ctx, operatorName, createPkgName, createPkgVersion)
			Expect(err).ToNot(HaveOccurred())

			By("Eventually reporting a successful resolution and bundle path")
			checkResolutionAndBundlePath(operator, createPkgVersion)

			By("Eventually installing the operator successfully")
			checkOperatorInstalled(operator)

			By("Eventually installing the package successfully")
			checkPackageInstalled(operator)

			By("Eventually verifying the presence of relevant manifest on cluster from the bundle")
			checkManifestPresence(createPkgName, createPkgVersion)
		})
	})
	When("An operator is upgraded to a higher version", func() {
		It("Upgrade to an higher version of the operator", func() {
			operator, err = updateOperator(ctx, operatorName, updatePkgName, updatePkgVersion)
			Expect(err).ToNot(HaveOccurred())

			By("Eventually reporting a successful resolution and bundle path for the upgraded version")
			checkResolutionAndBundlePath(operator, updatePkgVersion)

			By("Eventually upgrading the operator successfully")
			checkOperatorInstalled(operator)

			By("Eventually upgrading the package successfully")
			checkPackageInstalled(operator)

			By("Eventually verifying the presence of relevant manifest on cluster from the bundle")
			checkManifestPresence(createPkgName, createPkgVersion)

		})
	})
	When("An operator is deleted", func() {
		It("Delete and operator", func() {
			operator, err = deleteOperator(ctx, operatorName)
			Expect(err).ToNot(HaveOccurred())

			By("Eventually the operator should not exists")
			checkOperatorDeleted(operator)
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
	if err != nil {
		return err
	}

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

func checkCatalogUnpacked(operatorCatalog *catalogd.Catalog) {
	Eventually(func(g Gomega) {
		err := c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, operatorCatalog)
		g.Expect(err).ToNot(HaveOccurred())
		cond := apimeta.FindStatusCondition(operatorCatalog.Status.Conditions, catalogd.TypeUnpacked)
		g.Expect(cond).ToNot(BeNil())
		g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(cond.Reason).To(Equal(catalogd.ReasonUnpackSuccessful))
	}, 10*time.Second, 1).Should(Succeed())
}

func checkPackageCreated() {
	Eventually(func(g Gomega) {
		pList := &catalogd.PackageList{}
		err = c.List(ctx, pList)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(pList.Items).ToNot(BeEmpty()) // Checking if atleast one package is created
	}).Should(Succeed())
}

func checkBundleMetadataCreated() {
	Eventually(func(g Gomega) {
		bmList := &catalogd.BundleMetadataList{}
		err = c.List(ctx, bmList)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(bmList.Items).ToNot(BeEmpty()) // Checking if atleast one bundle metadata is created
	}).Should(Succeed())
}

func checkResolutionAndBundlePath(operator *operatorv1alpha1.Operator, operatorVersion string) {
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
		g.Expect(operator.Status.Conditions).To(HaveLen(2))
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
		g.Expect(cond).ToNot(BeNil())
		g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
		g.Expect(cond.Message).To(ContainSubstring("resolved to"))
		g.Expect(operator.Status.ResolvedBundleResource).ToNot(BeEmpty())
		g.Expect(operator.Spec.Version).To(Equal(operatorVersion))
	}, 10*time.Second, 1).Should(Succeed())
}

func checkOperatorInstalled(operator *operatorv1alpha1.Operator) {
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeInstalled)
		g.Expect(cond).ToNot(BeNil())
		g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
		g.Expect(cond.Message).To(ContainSubstring("installed from"))
		g.Expect(operator.Status.InstalledBundleResource).ToNot(BeEmpty())
	}, 10*time.Second, 1).Should(Succeed())
}

func checkPackageInstalled(operator *operatorv1alpha1.Operator) {
	Eventually(func(g Gomega) {
		bd := rukpakv1alpha1.BundleDeployment{}
		g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, &bd)).To(Succeed())
		g.Expect(bd.Status.Conditions).To(HaveLen(2))
		g.Expect(bd.Status.Conditions[0].Reason).To(Equal("UnpackSuccessful"))
		g.Expect(bd.Status.Conditions[1].Reason).To(Equal("InstallationSucceeded"))
	}, 10*time.Second, 1).Should(Succeed())
}

func checkManifestPresence(packageName string, version string) {
	Eventually(func(g Gomega) {
		resources, _ := collectKubernetesObjects(packageName, version)
		for _, resource := range resources {
			gvk := schema.GroupVersionKind{
				Group:   "",
				Version: resource.APIVersion,
				Kind:    resource.Kind,
			}

			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(gvk)
			g.Expect(c.Get(ctx, types.NamespacedName{Name: resource.Metadata.Name, Namespace: nameSpace}, obj)).To(Succeed())
		}
	}).Should(Succeed())
}

func checkOperatorDeleted(operator *operatorv1alpha1.Operator) {
	Eventually(func(g Gomega) {
		err := c.Get(ctx, types.NamespacedName{Name: operator.Name}, &operatorv1alpha1.Operator{})
		g.Expect(errors.IsNotFound(err)).To(BeTrue())
	}).Should(Succeed())
}

func createTestCatalog(ctx context.Context, name string, imageRef string) (*catalogd.Catalog, error) {
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

func createOperator(ctx context.Context, operatorName string, packageName string, version string) (*operatorv1alpha1.Operator, error) {
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

func updateOperator(ctx context.Context, operatorName string, packageName string, version string) (*operatorv1alpha1.Operator, error) {
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
