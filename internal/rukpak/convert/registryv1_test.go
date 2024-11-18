package convert

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/property"
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
				resObj := findObjectByName(svc.Name, plainBundle.Objects)
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
				resObj := findObjectByName(svc.Name, plainBundle.Objects)
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
					resObj := findObjectByName(pc.Name, plainBundle.Objects)
					Expect(resObj).NotTo(BeNil())
					Expect(resObj.GetNamespace()).To(BeEmpty())
				})
			})
		})

		Context("Should generate objects successfully based on target namespaces", func() {
			var (
				svc             corev1.Service
				baseCSV         v1alpha1.ClusterServiceVersion
				watchNamespaces []string
			)

			BeforeEach(func() {
				// base CSV definition that each test case will deep copy and modify
				baseCSV = v1alpha1.ClusterServiceVersion{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testCSV",
						Annotations: map[string]string{
							"olm.properties": fmt.Sprintf("[{\"type\": %s, \"value\": \"%s\"}]", property.TypeConstraint, "value"),
						},
					},
					Spec: v1alpha1.ClusterServiceVersionSpec{
						InstallStrategy: v1alpha1.NamedInstallStrategy{
							StrategySpec: v1alpha1.StrategyDetailsDeployment{
								DeploymentSpecs: []v1alpha1.StrategyDeploymentSpec{
									{
										Name: "testDeployment",
										Spec: appsv1.DeploymentSpec{
											Template: corev1.PodTemplateSpec{
												Spec: corev1.PodSpec{
													Containers: []corev1.Container{
														{
															Name:  "testContainer",
															Image: "testImage",
														},
													},
												},
											},
										},
									},
								},
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

			It("should convert into plain manifests successfully with AllNamespaces", func() {
				csv := baseCSV.DeepCopy()
				csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true}}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{""}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         *csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).NotTo(HaveOccurred())

				By("verifying if plain bundle has required objects")
				Expect(plainBundle).ShouldNot(BeNil())
				Expect(plainBundle.Objects).To(HaveLen(5))

				By("verifying olm.targetNamespaces annotation in the deployment's pod template")
				dep := findObjectByName("testDeployment", plainBundle.Objects)
				Expect(dep).NotTo(BeNil())
				Expect(dep.(*appsv1.Deployment).Spec.Template.Annotations).To(HaveKeyWithValue("olm.targetNamespaces", strings.Join(watchNamespaces, ",")))
			})

			It("should convert into plain manifests successfully with MultiNamespace", func() {
				csv := baseCSV.DeepCopy()
				csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true}}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{"testWatchNs1", "testWatchNs2"}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         *csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).NotTo(HaveOccurred())

				By("verifying if plain bundle has required objects")
				Expect(plainBundle).ShouldNot(BeNil())
				Expect(plainBundle.Objects).To(HaveLen(7))

				By("verifying olm.targetNamespaces annotation in the deployment's pod template")
				dep := findObjectByName("testDeployment", plainBundle.Objects)
				Expect(dep).NotTo(BeNil())
				Expect(dep.(*appsv1.Deployment).Spec.Template.Annotations).To(HaveKeyWithValue("olm.targetNamespaces", strings.Join(watchNamespaces, ",")))
			})

			It("should convert into plain manifests successfully with SingleNamespace", func() {
				csv := baseCSV.DeepCopy()
				csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true}}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{"testWatchNs1"}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         *csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).NotTo(HaveOccurred())

				By("verifying if plain bundle has required objects")
				Expect(plainBundle).ShouldNot(BeNil())
				Expect(plainBundle.Objects).To(HaveLen(5))

				By("verifying olm.targetNamespaces annotation in the deployment's pod template")
				dep := findObjectByName("testDeployment", plainBundle.Objects)
				Expect(dep).NotTo(BeNil())
				Expect(dep.(*appsv1.Deployment).Spec.Template.Annotations).To(HaveKeyWithValue("olm.targetNamespaces", strings.Join(watchNamespaces, ",")))
			})

			It("should convert into plain manifests successfully with own namespace", func() {
				csv := baseCSV.DeepCopy()
				csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true}}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{installNamespace}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         *csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).NotTo(HaveOccurred())

				By("verifying if plain bundle has required objects")
				Expect(plainBundle).ShouldNot(BeNil())
				Expect(plainBundle.Objects).To(HaveLen(5))

				By("verifying olm.targetNamespaces annotation in the deployment's pod template")
				dep := findObjectByName("testDeployment", plainBundle.Objects)
				Expect(dep).NotTo(BeNil())
				Expect(dep.(*appsv1.Deployment).Spec.Template.Annotations).To(HaveKeyWithValue("olm.targetNamespaces", strings.Join(watchNamespaces, ",")))
			})

			It("should error when multinamespace mode is supported with an empty string in target namespaces", func() {
				csv := baseCSV.DeepCopy()
				csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true}}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{"testWatchNs1", ""}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         *csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).To(HaveOccurred())
				Expect(plainBundle).To(BeNil())
			})

			It("should error when single namespace mode is disabled with more than one target namespaces", func() {
				csv := baseCSV.DeepCopy()
				csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: false}}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{"testWatchNs1", "testWatchNs2"}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         *csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).To(HaveOccurred())
				Expect(plainBundle).To(BeNil())
			})

			It("should error when all namespace mode is disabled with target namespace containing an empty string", func() {
				csv := baseCSV.DeepCopy()
				csv.Spec.InstallModes = []v1alpha1.InstallMode{
					{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: false},
					{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true},
					{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true},
					{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true},
				}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{""}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         *csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to plain")
				plainBundle, err := Convert(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).To(HaveOccurred())
				Expect(plainBundle).To(BeNil())
			})

			It("should propagate csv annotations to chart metadata annotation", func() {
				csv := baseCSV.DeepCopy()
				csv.Spec.InstallModes = []v1alpha1.InstallMode{{Type: v1alpha1.InstallModeTypeMultiNamespace, Supported: true}}

				By("creating a registry v1 bundle")
				watchNamespaces = []string{"testWatchNs1", "testWatchNs2"}
				unstructuredSvc := convertToUnstructured(svc)
				registryv1Bundle = RegistryV1{
					PackageName: "testPkg",
					CSV:         *csv,
					Others:      []unstructured.Unstructured{unstructuredSvc},
				}

				By("converting to helm")
				chrt, err := toChart(registryv1Bundle, installNamespace, watchNamespaces)
				Expect(err).NotTo(HaveOccurred())
				Expect(chrt.Metadata.Annotations["olm.properties"]).NotTo(BeNil())
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

func findObjectByName(name string, result []client.Object) client.Object {
	for _, o := range result {
		// Since this is a controlled env, comparing only the names is sufficient for now.
		// In future, compare GVKs too by ensuring its set on the unstructuredObj.
		if o.GetName() == name {
			return o
		}
	}
	return nil
}
