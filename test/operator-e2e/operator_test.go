package operator_e2e

import (
	"context"
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
	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

var (
	cfg             *rest.Config
	c               client.Client
	operatorCatalog *catalogd.Catalog
	operator        *operatorv1alpha1.Operator
	err             error
	ctx             context.Context
)

const (
	testCatalogRef   = "localhost/testdata/catalogs/plainv0-test-catalog:e2e"
	testCatalogName  = "plainv0-test-catalog"
	testOperatorName = "plainv0-test-operator"
	createPkgName    = "plain"
	createPkgVersion = "0.1.0"
	updatePkgName    = "plain"
	updatePkgVersion = "0.1.1"
	nameSpace        = "rukpak-system"
)

func TestOperatorCreateUpgradeDelete(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operator Creation, Upgradation and Deletion Suite")
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

var _ = Describe("Operator creation, upgradtion and deletion", func() {
	When("A catalog object is created from the FBC image and deployed", func() {
		It("Deploy catalog object with FBC image", func() {
			By("Creating a operator catalog object")
			operatorCatalog, err = createTestCatalog(ctx, testCatalogName, testCatalogRef)
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
			operator, err = createOperator(ctx, testOperatorName, createPkgName, createPkgVersion)
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
			operator, err = updateOperator(ctx, testOperatorName, updatePkgName, updatePkgVersion)
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
			operator, err = deleteOperator(ctx, testOperatorName)
			Expect(err).ToNot(HaveOccurred())

			By("Eventually the operator should not exists")
			checkOperatorDeleted(operator)
		})
	})
})

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
		g.Expect(len(pList.Items)).To(BeNumerically(">=", 1)) // Checking if atleast one package is created
	}).Should(Succeed())
}

func checkBundleMetadataCreated() {
	Eventually(func(g Gomega) {
		bmList := &catalogd.BundleMetadataList{}
		err = c.List(ctx, bmList)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(len(bmList.Items)).To(BeNumerically(">=", 1)) // Checking if atleast one bundle metadata is created
	}).Should(Succeed())
}

func checkResolutionAndBundlePath(operator *operatorv1alpha1.Operator, operatorVersion string) {
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
		g.Expect(len(operator.Status.Conditions)).To(Equal(2))
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
		g.Expect(len(bd.Status.Conditions)).To(Equal(2))
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
