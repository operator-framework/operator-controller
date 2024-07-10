package convert

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

func TestRegistryV1Converter(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RegstryV1 suite")
}

var _ = Describe("RegistryV1 Suite", func() {
	var _ = Describe("Convert", func() {
		var (
			registryv1Bundle RegistryV1
			installNamespace string
			targetNamespaces []string
		)
		Context("Should set the namespaces of object correctly", func() {
			var (
				svc corev1.Service
				csv v1alpha1.ClusterServiceVersion
			)
			BeforeEach(func() {
				csv = v1alpha1.ClusterServiceVersion{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testCSV",
					},
					Spec: v1alpha1.ClusterServiceVersionSpec{
						InstallModes: []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true}},
					},
				}
				svc = corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testService",
					},
				}
				svc.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"})
				installNamespace = "testInstallNamespace"
			})

			It("should set the namespace to installnamespace if not available", func() {
				By("creating a registry v1 bundle")
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, targetNamespaces)
				Expect(err).NotTo(HaveOccurred())

				By("verifying if plain bundle has required objects")
				Expect(plainBundle).NotTo(BeNil())
				Expect(plainBundle.Objects).To(HaveLen(1))

				By("verifying if ns has been set correctly")
				resObj := containsObject(unstructuredSvc, plainBundle.Objects)
				Expect(resObj).NotTo(BeNil())
				Expect(resObj.GetNamespace()).To(BeEquivalentTo(installNamespace))
			})

			It("should override namespace if already available", func() {
				By("creating a registry v1 bundle")
				svc.SetNamespace("otherNs")
				unstructuredSvc := convertToUnstructured(svc)
				unstructuredSvc.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"})

				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, targetNamespaces)
				Expect(err).NotTo(HaveOccurred())

				By("verifying if plain bundle has required objects")
				Expect(plainBundle).NotTo(BeNil())
				Expect(plainBundle.Objects).To(HaveLen(1))

				By("verifying if ns has been set correctly")
				resObj := containsObject(unstructuredSvc, plainBundle.Objects)
				Expect(resObj).NotTo(BeNil())
				Expect(resObj.GetNamespace()).To(BeEquivalentTo(installNamespace))
			})

			Context("Should error when object is not supported", func() {
				It("should error when unsupported GVK is passed", func() {
					By("creating an unsupported kind")
					event := corev1.Event{
						ObjectMeta: metav1.ObjectMeta{
							Name: "testEvent",
						},
					}

					unstructuredEvt := convertToUnstructured(event)
					unstructuredEvt.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Event"})

					registryv1Bundle = RegistryV1{
						PackageName: "testPkg",
						CSV:         csv,
						Others:      []unstructured.Unstructured{unstructuredEvt},
					}

					By("converting to plain")
					plainBundle, err := Convert(registryv1Bundle, installNamespace, targetNamespaces)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("bundle contains unsupported resource"))
					Expect(plainBundle).To(BeNil())
				})
			})

			Context("Should not set ns cluster scoped object is passed", func() {
				It("should not error when cluster scoped obj is passed and not set its namespace", func() {
					By("creating an unsupported kind")
					pc := schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "testPriorityClass",
						},
					}

					unstructuredpriorityclass := convertToUnstructured(pc)
					unstructuredpriorityclass.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "PriorityClass"})

					registryv1Bundle = RegistryV1{
						PackageName: "testPkg",
						CSV:         csv,
						Others:      []unstructured.Unstructured{unstructuredpriorityclass},
					}

					By("converting to plain")
					plainBundle, err := Convert(registryv1Bundle, installNamespace, targetNamespaces)
					Expect(err).NotTo(HaveOccurred())

					By("verifying if plain bundle has required objects")
					Expect(plainBundle).NotTo(BeNil())
					Expect(plainBundle.Objects).To(HaveLen(1))

					By("verifying if ns has been set correctly")
					resObj := containsObject(unstructuredpriorityclass, plainBundle.Objects)
					Expect(resObj).NotTo(BeNil())
					Expect(resObj.GetNamespace()).To(BeEmpty())
				})
			})
		})

		Context("Should generate objects successfully based on target namespaces", func() {
			var (
				svc             corev1.Service
				csv             v1alpha1.ClusterServiceVersion
				watchNamespaces []string
			)

			BeforeEach(func() {
				csv = v1alpha1.ClusterServiceVersion{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testCSV",
					},
					Spec: v1alpha1.ClusterServiceVersionSpec{
						InstallModes: []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true}},
						InstallStrategy: v1alpha1.NamedInstallStrategy{
							StrategySpec: v1alpha1.StrategyDetailsDeployment{
								Permissions: []v1alpha1.StrategyDeploymentPermissions{
									{
										ServiceAccountName: "testServiceAccount",
										Rules: []rbacv1.PolicyRule{
											{
												APIGroups: []string{"test"},
												Resources: []string{"pods"},
												Verbs:     []string{"*"},
											},
										},
									},
								},
							},
						},
					},
				}
				svc = corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testService",
					},
				}
				svc.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"})
				installNamespace = "testInstallNamespace"
			})

			It("should convert into plain manifests successfully", func() {
				By("creating a registry v1 bundle")
				watchNamespaces = []string{"testWatchNs1", "testWatchNs2"}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).NotTo(HaveOccurred())

				By("verifying if plain bundle has required objects")
				Expect(plainBundle).ShouldNot(BeNil())
				Expect(plainBundle.Objects).To(HaveLen(6))
			})

			It("should convert into plain manifests successfully with single namespace", func() {
				csv = v1alpha1.ClusterServiceVersion{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testCSV",
					},
					Spec: v1alpha1.ClusterServiceVersionSpec{
						InstallModes: []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true}},
						InstallStrategy: v1alpha1.NamedInstallStrategy{
							StrategySpec: v1alpha1.StrategyDetailsDeployment{
								Permissions: []v1alpha1.StrategyDeploymentPermissions{
									{
										ServiceAccountName: "testServiceAccount",
										Rules: []rbacv1.PolicyRule{
											{
												APIGroups: []string{"test"},
												Resources: []string{"pods"},
												Verbs:     []string{"*"},
											},
										},
									},
								},
							},
						},
					},
				}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{"testWatchNs1"}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).NotTo(HaveOccurred())

				By("verifying if plain bundle has required objects")
				Expect(plainBundle).ShouldNot(BeNil())
				Expect(plainBundle.Objects).To(HaveLen(4))
			})

			It("should convert into plain manifests successfully with own namespace", func() {
				csv = v1alpha1.ClusterServiceVersion{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testCSV",
					},
					Spec: v1alpha1.ClusterServiceVersionSpec{
						InstallModes: []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true}},
						InstallStrategy: v1alpha1.NamedInstallStrategy{
							StrategySpec: v1alpha1.StrategyDetailsDeployment{
								Permissions: []v1alpha1.StrategyDeploymentPermissions{
									{
										ServiceAccountName: "testServiceAccount",
										Rules: []rbacv1.PolicyRule{
											{
												APIGroups: []string{"test"},
												Resources: []string{"pods"},
												Verbs:     []string{"*"},
											},
										},
									},
								},
							},
						},
					},
				}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{installNamespace}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).NotTo(HaveOccurred())

				By("verifying if plain bundle has required objects")
				Expect(plainBundle).ShouldNot(BeNil())
				Expect(plainBundle.Objects).To(HaveLen(4))
			})

			It("should error when multinamespace mode is supported with an empty string in target namespaces", func() {
				By("creating a registry v1 bundle")
				watchNamespaces = []string{"testWatchNs1", ""}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).To(HaveOccurred())
				Expect(plainBundle).To(BeNil())
			})

			It("should error when single namespace mode is disabled with more than one target namespaces", func() {
				csv = v1alpha1.ClusterServiceVersion{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testCSV",
					},
					Spec: v1alpha1.ClusterServiceVersionSpec{
						InstallModes: []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: false}},
					},
				}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{"testWatchNs1", "testWatchNs2"}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).To(HaveOccurred())
				Expect(plainBundle).To(BeNil())
			})

			It("should error when all namespace mode is disabled with target namespace containing an empty string", func() {
				csv = v1alpha1.ClusterServiceVersion{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testCSV",
					},
					Spec: v1alpha1.ClusterServiceVersionSpec{
						InstallModes: []v1alpha1.InstallMode{
							{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: false},
							{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true},
							{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true},
							{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true},
						},
					},
				}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{""}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).To(HaveOccurred())
				Expect(plainBundle).To(BeNil())
			})
		})

		Context("Should enforce limitations", func() {
			It("should not allow bundles with webhooks", func() {
				By("creating a registry v1 bundle")
				csv := v1alpha1.ClusterServiceVersion{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testCSV",
					},
					Spec: v1alpha1.ClusterServiceVersionSpec{
						InstallModes:       []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true}},
						WebhookDefinitions: []v1alpha1.WebhookDescription{{ConversionCRDs: []string{"fake-webhook.package-with-webhooks.io"}}},
					},
				}
				watchNamespaces := []string{metav1.NamespaceAll}
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         csv,
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).To(MatchError(ContainSubstring("webhookDefinitions are not supported")))
				Expect(plainBundle).To(BeNil())
			})

			It("should not allow bundles with API service definitions", func() {
				By("creating a registry v1 bundle")
				csv := v1alpha1.ClusterServiceVersion{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testCSV",
					},
					Spec: v1alpha1.ClusterServiceVersionSpec{
						InstallModes: []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true}},
						APIServiceDefinitions: v1alpha1.APIServiceDefinitions{
							Owned: []v1alpha1.APIServiceDescription{{Name: "fake-owned-api-definition"}},
						},
					},
				}
				watchNamespaces := []string{metav1.NamespaceAll}
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         csv,
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).To(MatchError(ContainSubstring("apiServiceDefintions are not supported")))
				Expect(plainBundle).To(BeNil())
			})
		})
	})
})

func convertToUnstructured(obj interface{}) unstructured.Unstructured {
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&obj)
	Expect(err).NotTo(HaveOccurred())
	Expect(unstructuredObj).NotTo(BeNil())
	return unstructured.Unstructured{Object: unstructuredObj}
}

func containsObject(obj unstructured.Unstructured, result []client.Object) client.Object {
	for _, o := range result {
		// Since this is a controlled env, comparing only the names is sufficient for now.
		// In future, compare GVKs too by ensuring its set on the unstructuredObj.
		if o.GetName() == obj.GetName() {
			return o
		}
	}
	return nil
}
