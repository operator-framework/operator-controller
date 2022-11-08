package e2e

import (
	"context"
	"fmt"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var _ = Describe("operators controller", func() {
	var (
		ctx context.Context
		ns  *corev1.Namespace
	)
	BeforeEach(func() {
		ctx = context.Background()
		ns = SetupTestNamespace(c, genName("e2e-"))
	})
	AfterEach(func() {
		Expect(c.Delete(ctx, ns)).To(BeNil())
	})

	When("sourcing content from multiple catalog sources", func() {
		var (
			catalog1 MagicCatalog
			catalog2 MagicCatalog
		)
		BeforeEach(func() {
			provider, err := NewFileBasedFiledBasedCatalogProvider(filepath.Join(dataBaseDir, "prometheus.v0.2.0.yaml"))
			Expect(err).To(BeNil())

			catalog1 = NewMagicCatalog(c, ns.GetName(), "prometheus", provider)
			Expect(catalog1.DeployCatalog(ctx)).To(BeNil())

			provider2, err := NewFileBasedFiledBasedCatalogProvider(filepath.Join(dataBaseDir, "crossplane.v0.1.0.yaml"))
			Expect(err).To(BeNil())

			catalog2 = NewMagicCatalog(c, ns.GetName(), "crossplane", provider2)
			Expect(catalog2.DeployCatalog(ctx)).To(BeNil())
		})
		AfterEach(func() {
			Expect(catalog1.UndeployCatalog(ctx)).To(BeNil())
			Expect(catalog2.UndeployCatalog(ctx)).To(BeNil())
		})

		When("a valid operator is created", func() {
			var (
				o *operatorv1alpha1.Operator
			)
			BeforeEach(func() {
				o = &operatorv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "valid-",
					},
					Spec: operatorv1alpha1.OperatorSpec{
						Package: &operatorv1alpha1.PackageSpec{
							Name: "crossplane",
						},
					},
				}
				Expect(c.Create(ctx, o)).To(Succeed())
			})
			AfterEach(func() {
				Expect(HandleTestCaseFailure()).To(BeNil())
				Expect(c.Delete(ctx, o)).To(Succeed())
			})

			It("should chose the highest semver package when no catalog source configuration has been specified", func() {
				By("verifying the installation eventually succeeds")
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return nil, err
					}
					if o.Status.ActiveBundleDeployment.Name == "" {
						return nil, fmt.Errorf("waiting for bundledeployment name to be populated")
					}
					return meta.FindStatusCondition(o.Status.Conditions, operatorv1alpha1.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(operatorv1alpha1.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(operatorv1alpha1.ReasonInstallSuccessful)),
				))

				By("verifying that the highest semver container image was selected")
				Eventually(func() bool {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return false
					}
					bdName := o.Status.ActiveBundleDeployment.Name
					if bdName == "" {
						return false
					}

					bd := &rukpakv1alpha1.BundleDeployment{}
					if err := c.Get(ctx, types.NamespacedName{Name: bdName}, bd); err != nil {
						return false
					}
					return bd.Spec.Template.Spec.Source.Image.Ref == "quay.io/operatorhubio/universal-crossplane:v1.5.1-up.1"
				}).Should(BeTrue())

			})
		})
	})

	When("sourcing content from a single catalog source", func() {
		var (
			catalog MagicCatalog
		)
		BeforeEach(func() {
			provider, err := NewFileBasedFiledBasedCatalogProvider(filepath.Join(dataBaseDir, "prometheus.v0.2.0.yaml"))
			Expect(err).To(BeNil())

			catalog = NewMagicCatalog(c, ns.GetName(), "prometheus", provider)
			Expect(catalog.DeployCatalog(ctx)).To(BeNil())
		})
		AfterEach(func() {
			Expect(catalog.UndeployCatalog(ctx)).To(BeNil())
		})

		When("an operator has been created with an explicit version specified", func() {
			var (
				o *operatorv1alpha1.Operator
			)
			BeforeEach(func() {
				o = &operatorv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "valid-",
					},
					Spec: operatorv1alpha1.OperatorSpec{
						Catalog: &operatorv1alpha1.CatalogSpec{
							Name:      "prometheus",
							Namespace: ns.GetName(),
						},
						Package: &operatorv1alpha1.PackageSpec{
							Name:    "prometheus-operator",
							Version: "0.1.0",
						},
					},
				}
				Expect(c.Create(ctx, o)).To(Succeed())
			})
			AfterEach(func() {
				Expect(HandleTestCaseFailure()).To(BeNil())
				Expect(c.Delete(ctx, o)).To(Succeed())
			})

			It("should eventually result in a successful installation", func() {
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return nil, err
					}
					if o.Status.ActiveBundleDeployment.Name == "" {
						return nil, fmt.Errorf("waiting for bundledeployment name to be populated")
					}
					return meta.FindStatusCondition(o.Status.Conditions, operatorv1alpha1.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(operatorv1alpha1.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(operatorv1alpha1.ReasonInstallSuccessful)),
				))
			})

			It("should generate a BundleDeployment that matches the expected version", func() {
				Eventually(func() bool {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return false
					}
					bdName := o.Status.ActiveBundleDeployment.Name
					if bdName == "" {
						return false
					}

					bd := &rukpakv1alpha1.BundleDeployment{}
					if err := c.Get(ctx, types.NamespacedName{Name: bdName}, bd); err != nil {
						return false
					}
					// Note: this points to the v0.1.0 image.
					return bd.Spec.Template.Spec.Source.Image.Ref == "quay.io/operatorhubio/prometheus:v0.47.0"
				}).Should(BeTrue())
			})
		})

		When("an operator has been created with a version range specified", func() {
			var (
				o *operatorv1alpha1.Operator
			)
			BeforeEach(func() {
				o = &operatorv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "valid-",
					},
					Spec: operatorv1alpha1.OperatorSpec{
						Catalog: &operatorv1alpha1.CatalogSpec{
							Name:      "prometheus",
							Namespace: ns.GetName(),
						},
						Package: &operatorv1alpha1.PackageSpec{
							Name:    "prometheus-operator",
							Version: ">0.1.0",
						},
					},
				}
				Expect(c.Create(ctx, o)).To(Succeed())
			})
			AfterEach(func() {
				Expect(HandleTestCaseFailure()).To(BeNil())
				Expect(c.Delete(ctx, o)).To(Succeed())
			})

			It("should eventually result in a successful installation", func() {
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return nil, err
					}
					if o.Status.ActiveBundleDeployment.Name == "" {
						return nil, fmt.Errorf("waiting for bundledeployment name to be populated")
					}
					return meta.FindStatusCondition(o.Status.Conditions, operatorv1alpha1.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(operatorv1alpha1.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(operatorv1alpha1.ReasonInstallSuccessful)),
				))
			})

			It("should generate a BundleDeployment that matches the expected version", func() {
				Eventually(func() bool {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return false
					}
					bdName := o.Status.ActiveBundleDeployment.Name
					if bdName == "" {
						return false
					}

					bd := &rukpakv1alpha1.BundleDeployment{}
					if err := c.Get(ctx, types.NamespacedName{Name: bdName}, bd); err != nil {
						return false
					}
					// Note: this points to the v0.2.0 image.
					return bd.Spec.Template.Spec.Source.Image.Ref == "quay.io/operatorhubio/prometheus:v0.47.0--20220413T184225"
				}).Should(BeTrue())
			})
		})

		When("a valid operator is created", func() {
			var (
				o *operatorv1alpha1.Operator
			)
			BeforeEach(func() {
				o = &operatorv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "valid-",
					},
					Spec: operatorv1alpha1.OperatorSpec{
						Catalog: &operatorv1alpha1.CatalogSpec{
							Name:      "prometheus",
							Namespace: ns.GetName(),
						},
						Package: &operatorv1alpha1.PackageSpec{
							Name: "prometheus-operator",
						},
					},
				}
				Expect(c.Create(ctx, o)).To(Succeed())
			})
			AfterEach(func() {
				Expect(HandleTestCaseFailure()).To(BeNil())
				Expect(c.Delete(ctx, o)).To(Succeed())
			})

			It("should eventually contain a non-empty status.ActiveBundleDeployment.Name", func() {
				Eventually(func() (bool, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return false, err
					}
					return o.Status.ActiveBundleDeployment.Name != "", nil
				})
			})

			It("should eventually result in a successful installation", func() {
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return nil, err
					}
					if o.Status.ActiveBundleDeployment.Name == "" {
						return nil, fmt.Errorf("waiting for bundledeployment name to be populated")
					}
					return meta.FindStatusCondition(o.Status.Conditions, operatorv1alpha1.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(operatorv1alpha1.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(operatorv1alpha1.ReasonInstallSuccessful)),
				))
			})

			When("a new operator version has become available", func() {
				BeforeEach(func() {
					updatedProvider, err := NewFileBasedFiledBasedCatalogProvider(filepath.Join(dataBaseDir, "prometheus.v0.3.0.yaml"))
					Expect(err).To(BeNil())

					Expect(catalog.UpdateCatalog(ctx, updatedProvider)).To(BeNil())
				})
				It("should eventually pivot to the most recent version", func() {
					Eventually(func() bool {
						if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
							return false
						}
						bdName := o.Status.ActiveBundleDeployment.Name
						if bdName == "" {
							return false
						}

						bd := &rukpakv1alpha1.BundleDeployment{}
						if err := c.Get(ctx, types.NamespacedName{Name: bdName}, bd); err != nil {
							return false
						}
						version, ok := bd.GetLabels()["core.olm.io/version"]
						if !ok {
							return false
						}
						return version == "0.3.0" && bd.Spec.Template.Spec.Source.Image.Ref == "quay.io/operatorhubio/prometheus:v0.47.0--20220325T220130"
					}).Should(BeTrue())
				})
			})
		})
	})

	When("multiple catalogs contain the same package name", func() {
		var (
			catalog1 MagicCatalog
			catalog2 MagicCatalog
		)
		BeforeEach(func() {
			provider, err := NewFileBasedFiledBasedCatalogProvider(filepath.Join(dataBaseDir, "prometheus.v0.1.0.yaml"))
			Expect(err).To(BeNil())

			catalog1 = NewMagicCatalog(c, ns.GetName(), "prometheus-1", provider)
			Expect(catalog1.DeployCatalog(ctx)).To(BeNil())

			provider2, err := NewFileBasedFiledBasedCatalogProvider(filepath.Join(dataBaseDir, "prometheus.v0.2.0.yaml"))
			Expect(err).To(BeNil())

			catalog2 = NewMagicCatalog(c, ns.GetName(), "prometheus-2", provider2)
			Expect(catalog2.DeployCatalog(ctx)).To(BeNil())
		})
		AfterEach(func() {
			Expect(catalog1.UndeployCatalog(ctx)).To(BeNil())
			Expect(catalog2.UndeployCatalog(ctx)).To(BeNil())
		})

		When("an operator is created that doesn't specify a catalog", func() {
			var (
				o *operatorv1alpha1.Operator
			)
			BeforeEach(func() {
				o = &operatorv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "valid-",
					},
					Spec: operatorv1alpha1.OperatorSpec{
						Package: &operatorv1alpha1.PackageSpec{
							Name: "prometheus-operator",
						},
					},
				}
				Expect(c.Create(ctx, o)).To(Succeed())
			})
			AfterEach(func() {
				Expect(HandleTestCaseFailure()).To(BeNil())
				Expect(c.Delete(ctx, o)).To(Succeed())
			})

			It("should eventually select the catalog that contains the highest semver bundle", func() {
				Eventually(func() (*operatorv1alpha1.SourceInfo, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return nil, err
					}
					return &o.Status.SourceInfo, nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *operatorv1alpha1.SourceInfo) string { return c.Name }, Equal("prometheus-2")),
					WithTransform(func(c *operatorv1alpha1.SourceInfo) string { return c.Namespace }, Equal(ns.GetName())),
				))
			})

			When("a new operator version has become available in catalog1", func() {
				BeforeEach(func() {
					updatedProvider, err := NewFileBasedFiledBasedCatalogProvider(filepath.Join(dataBaseDir, "prometheus.v0.3.0.yaml"))
					Expect(err).To(BeNil())

					Expect(catalog1.UpdateCatalog(ctx, updatedProvider)).To(BeNil())
				})
				It("should avoid pivoting to another catalog after initial installation", func() {
					Consistently(func() (*operatorv1alpha1.SourceInfo, error) {
						if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
							return nil, err
						}
						return &o.Status.SourceInfo, nil
					}).Should(And(
						Not(BeNil()),
						WithTransform(func(c *operatorv1alpha1.SourceInfo) string { return c.Name }, Equal("prometheus-2")),
						WithTransform(func(c *operatorv1alpha1.SourceInfo) string { return c.Namespace }, Equal(ns.GetName())),
					))
				})
			})
		})
	})
})
