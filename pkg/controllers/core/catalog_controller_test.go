package core_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing/fstest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/catalogd/internal/source"
	"github.com/operator-framework/catalogd/pkg/controllers/core"
	"github.com/operator-framework/catalogd/pkg/features"
)

var _ source.Unpacker = &MockSource{}

// MockSource is a utility for mocking out an Unpacker source
type MockSource struct {
	// result is the result that should be returned when MockSource.Unpack is called
	result *source.Result

	// shouldError determines whether or not the MockSource should return an error when MockSource.Unpack is called
	shouldError bool
}

func (ms *MockSource) Unpack(_ context.Context, _ *v1alpha1.Catalog) (*source.Result, error) {
	if ms.shouldError {
		return nil, errors.New("mocksource error")
	}

	return ms.result, nil
}

var _ = Describe("Catalogd Controller Test", func() {
	format.MaxLength = 0
	var (
		ctx        context.Context
		reconciler *core.CatalogReconciler
		mockSource *MockSource
	)
	BeforeEach(func() {
		ctx = context.Background()
		mockSource = &MockSource{}
		reconciler = &core.CatalogReconciler{
			Client: cl,
			Unpacker: source.NewUnpacker(
				map[v1alpha1.SourceType]source.Unpacker{
					v1alpha1.SourceTypeImage: mockSource,
				},
			),
		}
	})

	When("the catalog does not exist", func() {
		It("returns no error", func() {
			res, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "non-existent"}})
			Expect(res).To(Equal(ctrl.Result{}))
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("setting up with controller manager", func() {
		var mgr ctrl.Manager
		BeforeEach(func() {
			var err error
			mgr, err = ctrl.NewManager(cfg, manager.Options{Scheme: sch})
			Expect(mgr).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})
		It("returns no error", func() {
			Expect(reconciler.SetupWithManager(mgr)).To(Succeed())
		})
	})

	When("the catalog exists", func() {
		var (
			catalog    *v1alpha1.Catalog
			catalogKey types.NamespacedName
		)
		BeforeEach(func() {
			catalogKey = types.NamespacedName{Name: fmt.Sprintf("catalogd-test-%s", rand.String(8))}
		})

		When("the catalog specifies an invalid source", func() {
			BeforeEach(func() {
				By("initializing cluster state")
				catalog = &v1alpha1.Catalog{
					ObjectMeta: metav1.ObjectMeta{Name: catalogKey.Name},
					Spec: v1alpha1.CatalogSpec{
						Source: v1alpha1.CatalogSource{
							Type: "invalid-source",
						},
					},
				}
				Expect(cl.Create(ctx, catalog)).To(Succeed())
			})

			AfterEach(func() {
				By("tearing down cluster state")
				Expect(cl.Delete(ctx, catalog)).To(Succeed())
			})

			It("should set unpacking status to failed and return an error", func() {
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).To(HaveOccurred())

				// get the catalog and ensure status is set properly
				cat := &v1alpha1.Catalog{}
				Expect(cl.Get(ctx, catalogKey, cat)).To(Succeed())
				Expect(cat.Status.ResolvedSource).To(BeNil())
				Expect(cat.Status.Phase).To(Equal(v1alpha1.PhaseFailing))
				cond := meta.FindStatusCondition(cat.Status.Conditions, v1alpha1.TypeUnpacked)
				Expect(cond).ToNot(BeNil())
				Expect(cond.Reason).To(Equal(v1alpha1.ReasonUnpackFailed))
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			})
		})

		When("the catalog specifies a valid source", func() {
			BeforeEach(func() {
				By("initializing cluster state")
				catalog = &v1alpha1.Catalog{
					ObjectMeta: metav1.ObjectMeta{Name: catalogKey.Name},
					Spec: v1alpha1.CatalogSpec{
						Source: v1alpha1.CatalogSource{
							Type: "image",
							Image: &v1alpha1.ImageSource{
								Ref: "somecatalog:latest",
							},
						},
					},
				}
				Expect(cl.Create(ctx, catalog)).To(Succeed())
			})

			AfterEach(func() {
				By("tearing down cluster state")
				Expect(cl.Delete(ctx, catalog)).To(Succeed())
			})

			When("unpacker returns source.Result with state == 'Pending'", func() {
				BeforeEach(func() {
					mockSource.shouldError = false
					mockSource.result = &source.Result{State: source.StatePending}
				})

				It("should update status to reflect the pending state", func() {
					res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
					Expect(res).To(Equal(ctrl.Result{}))
					Expect(err).ToNot(HaveOccurred())

					// get the catalog and ensure status is set properly
					cat := &v1alpha1.Catalog{}
					Expect(cl.Get(ctx, catalogKey, cat)).To(Succeed())
					Expect(cat.Status.ResolvedSource).To(BeNil())
					Expect(cat.Status.Phase).To(Equal(v1alpha1.PhasePending))
					cond := meta.FindStatusCondition(cat.Status.Conditions, v1alpha1.TypeUnpacked)
					Expect(cond).ToNot(BeNil())
					Expect(cond.Reason).To(Equal(v1alpha1.ReasonUnpackPending))
					Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				})
			})

			When("unpacker returns source.Result with state == 'Unpacking'", func() {
				BeforeEach(func() {
					mockSource.shouldError = false
					mockSource.result = &source.Result{State: source.StateUnpacking}
				})

				It("should update status to reflect the unpacking state", func() {
					res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
					Expect(res).To(Equal(ctrl.Result{}))
					Expect(err).ToNot(HaveOccurred())

					// get the catalog and ensure status is set properly
					cat := &v1alpha1.Catalog{}
					Expect(cl.Get(ctx, catalogKey, cat)).To(Succeed())
					Expect(cat.Status.ResolvedSource).To(BeNil())
					Expect(cat.Status.Phase).To(Equal(v1alpha1.PhaseUnpacking))
					cond := meta.FindStatusCondition(cat.Status.Conditions, v1alpha1.TypeUnpacked)
					Expect(cond).ToNot(BeNil())
					Expect(cond.Reason).To(Equal(v1alpha1.ReasonUnpacking))
					Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				})
			})

			When("unpacker returns source.Result with unknown state", func() {
				BeforeEach(func() {
					mockSource.shouldError = false
					mockSource.result = &source.Result{State: "unknown"}
				})

				It("should set unpacking status to failed and return an error", func() {
					res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
					Expect(res).To(Equal(ctrl.Result{}))
					Expect(err).To(HaveOccurred())

					// get the catalog and ensure status is set properly
					cat := &v1alpha1.Catalog{}
					Expect(cl.Get(ctx, catalogKey, cat)).To(Succeed())
					Expect(cat.Status.ResolvedSource).To(BeNil())
					Expect(cat.Status.Phase).To(Equal(v1alpha1.PhaseFailing))
					cond := meta.FindStatusCondition(cat.Status.Conditions, v1alpha1.TypeUnpacked)
					Expect(cond).ToNot(BeNil())
					Expect(cond.Reason).To(Equal(v1alpha1.ReasonUnpackFailed))
					Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				})
			})

			When("unpacker returns source.Result with state == 'Unpacked'", func() {
				var (
					testBundleName              = "webhook-operator.v0.0.1"
					testBundleImage             = "quay.io/olmtest/webhook-operator-bundle:0.0.3"
					testBundleRelatedImageName  = "test"
					testBundleRelatedImageImage = "testimage:latest"
					testBundleObjectData        = "dW5pbXBvcnRhbnQK"
					testPackageDefaultChannel   = "preview"
					testPackageName             = "webhook-operator"
					testChannelName             = "preview"
					testPackage                 = fmt.Sprintf(testPackageTemplate, testPackageDefaultChannel, testPackageName)
					testBundle                  = fmt.Sprintf(testBundleTemplate, testBundleImage, testBundleName, testPackageName, testBundleRelatedImageName, testBundleRelatedImageImage, testBundleObjectData)
					testChannel                 = fmt.Sprintf(testChannelTemplate, testPackageName, testChannelName, testBundleName)

					testBundleMetaName  string
					testPackageMetaName string
				)
				BeforeEach(func() {
					testBundleMetaName = fmt.Sprintf("%s-%s", catalog.Name, testBundleName)
					testPackageMetaName = fmt.Sprintf("%s-%s", catalog.Name, testPackageName)

					filesys := &fstest.MapFS{
						"bundle.yaml":  &fstest.MapFile{Data: []byte(testBundle), Mode: os.ModePerm},
						"package.yaml": &fstest.MapFile{Data: []byte(testPackage), Mode: os.ModePerm},
						"channel.yaml": &fstest.MapFile{Data: []byte(testChannel), Mode: os.ModePerm},
					}

					mockSource.shouldError = false
					mockSource.result = &source.Result{
						ResolvedSource: &catalog.Spec.Source,
						State:          source.StateUnpacked,
						FS:             filesys,
					}
				})

				It("should set unpacking status to 'unpacked'", func() {
					// reconcile
					res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
					Expect(res).To(Equal(ctrl.Result{}))
					Expect(err).ToNot(HaveOccurred())

					// get the catalog and ensure status is set properly
					cat := &v1alpha1.Catalog{}
					Expect(cl.Get(ctx, catalogKey, cat)).To(Succeed())
					Expect(cat.Status.ResolvedSource).ToNot(BeNil())
					Expect(cat.Status.Phase).To(Equal(v1alpha1.PhaseUnpacked))
					cond := meta.FindStatusCondition(cat.Status.Conditions, v1alpha1.TypeUnpacked)
					Expect(cond).ToNot(BeNil())
					Expect(cond.Reason).To(Equal(v1alpha1.ReasonUnpackSuccessful))
					Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				})

				When("PackagesBundleMetadataAPIs feature gate is enabled", func() {
					BeforeEach(func() {
						Expect(features.CatalogdFeatureGate.SetFromMap(map[string]bool{
							string(features.PackagesBundleMetadataAPIs): true,
							string(features.CatalogMetadataAPI):         false,
						})).NotTo(HaveOccurred())

						// reconcile
						res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
						Expect(res).To(Equal(ctrl.Result{}))
						Expect(err).ToNot(HaveOccurred())
					})

					AfterEach(func() {
						// clean up package
						pkg := &v1alpha1.Package{
							ObjectMeta: metav1.ObjectMeta{
								Name: testPackageMetaName,
							},
						}
						Expect(cl.Delete(ctx, pkg)).To(Succeed())

						// clean up bundlemetadata
						bm := &v1alpha1.BundleMetadata{
							ObjectMeta: metav1.ObjectMeta{
								Name: testBundleMetaName,
							},
						}
						Expect(cl.Delete(ctx, bm)).To(Succeed())
						Expect(features.CatalogdFeatureGate.SetFromMap(map[string]bool{
							string(features.PackagesBundleMetadataAPIs): false,
							string(features.CatalogMetadataAPI):         false,
						})).NotTo(HaveOccurred())
					})

					It("should create BundleMetadata resources", func() {
						// validate bundlemetadata resources
						bundlemetadatas := &v1alpha1.BundleMetadataList{}
						Expect(cl.List(ctx, bundlemetadatas)).To(Succeed())
						Expect(bundlemetadatas.Items).To(HaveLen(1))
						bundlemetadata := bundlemetadatas.Items[0]
						Expect(bundlemetadata.Name).To(Equal(testBundleMetaName))
						Expect(bundlemetadata.Spec.Image).To(Equal(testBundleImage))
						Expect(bundlemetadata.Spec.Catalog.Name).To(Equal(catalog.Name))
						Expect(bundlemetadata.Spec.Package).To(Equal(testPackageName))
						Expect(bundlemetadata.Spec.RelatedImages).To(HaveLen(1))
						Expect(bundlemetadata.Spec.RelatedImages[0].Name).To(Equal(testBundleRelatedImageName))
						Expect(bundlemetadata.Spec.RelatedImages[0].Image).To(Equal(testBundleRelatedImageImage))
						Expect(bundlemetadata.Spec.Properties).To(HaveLen(1))
					})

					// TODO (rashmigottipati): Add testing of Package sync process.
					It("should create Package resources", func() {
						// validate package resources
						packages := &v1alpha1.PackageList{}
						Expect(cl.List(ctx, packages)).To(Succeed())
						Expect(packages.Items).To(HaveLen(1))
						pack := packages.Items[0]
						Expect(pack.Name).To(Equal(testPackageMetaName))
						Expect(pack.Spec.DefaultChannel).To(Equal(testPackageDefaultChannel))
						Expect(pack.Spec.Catalog.Name).To(Equal(catalog.Name))
						Expect(pack.Spec.Channels).To(HaveLen(1))
						Expect(pack.Spec.Channels[0].Name).To(Equal(testChannelName))
						Expect(pack.Spec.Channels[0].Entries).To(HaveLen(1))
						Expect(pack.Spec.Channels[0].Entries[0].Name).To(Equal(testBundleName))
					})

					When("creating another Catalog", func() {
						var (
							tempCatalog             *v1alpha1.Catalog
							tempTestBundleMetaName  string
							tempTestPackageMetaName string
						)
						BeforeEach(func() {
							tempCatalog = &v1alpha1.Catalog{
								ObjectMeta: metav1.ObjectMeta{Name: "tempedout"},
								Spec: v1alpha1.CatalogSpec{
									Source: v1alpha1.CatalogSource{
										Type: "image",
										Image: &v1alpha1.ImageSource{
											Ref: "somecatalog:latest",
										},
									},
								},
							}

							tempTestBundleMetaName = fmt.Sprintf("%s-%s", tempCatalog.Name, testBundleName)
							tempTestPackageMetaName = fmt.Sprintf("%s-%s", tempCatalog.Name, testPackageName)

							Expect(cl.Create(ctx, tempCatalog)).To(Succeed())
							res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "tempedout"}})
							Expect(res).To(Equal(ctrl.Result{}))
							Expect(err).ToNot(HaveOccurred())
						})

						AfterEach(func() {
							Expect(cl.Delete(ctx, tempCatalog)).NotTo(HaveOccurred())
						})

						It("should not delete BundleMetadata belonging to a different catalog", func() {
							bundlemetadata := &v1alpha1.BundleMetadata{}
							Expect(cl.Get(ctx, client.ObjectKey{Name: testBundleMetaName}, bundlemetadata)).To(Succeed())
							Expect(bundlemetadata.Name).To(Equal(testBundleMetaName))
							Expect(bundlemetadata.Spec.Image).To(Equal(testBundleImage))
							Expect(bundlemetadata.Spec.Catalog.Name).To(Equal(catalog.Name))
							Expect(bundlemetadata.Spec.Package).To(Equal(testPackageName))
							Expect(bundlemetadata.Spec.RelatedImages).To(HaveLen(1))
							Expect(bundlemetadata.Spec.RelatedImages[0].Name).To(Equal(testBundleRelatedImageName))
							Expect(bundlemetadata.Spec.RelatedImages[0].Image).To(Equal(testBundleRelatedImageImage))
							Expect(bundlemetadata.Spec.Properties).To(HaveLen(1))

							bundlemetadata = &v1alpha1.BundleMetadata{}
							Expect(cl.Get(ctx, client.ObjectKey{Name: tempTestBundleMetaName}, bundlemetadata)).To(Succeed())
							Expect(bundlemetadata.Name).To(Equal(tempTestBundleMetaName))
							Expect(bundlemetadata.Spec.Image).To(Equal(testBundleImage))
							Expect(bundlemetadata.Spec.Catalog.Name).To(Equal(tempCatalog.Name))
							Expect(bundlemetadata.Spec.Package).To(Equal(testPackageName))
							Expect(bundlemetadata.Spec.RelatedImages).To(HaveLen(1))
							Expect(bundlemetadata.Spec.RelatedImages[0].Name).To(Equal(testBundleRelatedImageName))
							Expect(bundlemetadata.Spec.RelatedImages[0].Image).To(Equal(testBundleRelatedImageImage))
							Expect(bundlemetadata.Spec.Properties).To(HaveLen(1))
						})

						It("should not delete Packages belonging to a different catalog", func() {
							// validate package resources
							pack := &v1alpha1.Package{}
							Expect(cl.Get(ctx, client.ObjectKey{Name: testPackageMetaName}, pack)).To(Succeed())
							Expect(pack.Name).To(Equal(testPackageMetaName))
							Expect(pack.Spec.DefaultChannel).To(Equal(testPackageDefaultChannel))
							Expect(pack.Spec.Catalog.Name).To(Equal(catalog.Name))
							Expect(pack.Spec.Channels).To(HaveLen(1))
							Expect(pack.Spec.Channels[0].Name).To(Equal(testChannelName))
							Expect(pack.Spec.Channels[0].Entries).To(HaveLen(1))
							Expect(pack.Spec.Channels[0].Entries[0].Name).To(Equal(testBundleName))

							pack = &v1alpha1.Package{}
							Expect(cl.Get(ctx, client.ObjectKey{Name: tempTestPackageMetaName}, pack)).To(Succeed())
							Expect(pack.Name).To(Equal(tempTestPackageMetaName))
							Expect(pack.Spec.DefaultChannel).To(Equal(testPackageDefaultChannel))
							Expect(pack.Spec.Catalog.Name).To(Equal(tempCatalog.Name))
							Expect(pack.Spec.Channels).To(HaveLen(1))
							Expect(pack.Spec.Channels[0].Name).To(Equal(testChannelName))
							Expect(pack.Spec.Channels[0].Entries).To(HaveLen(1))
							Expect(pack.Spec.Channels[0].Entries[0].Name).To(Equal(testBundleName))
						})
					})
				})

				When("the CatalogMetadataAPI feature gate is enabled", func() {
					BeforeEach(func() {
						Expect(features.CatalogdFeatureGate.SetFromMap(map[string]bool{
							string(features.CatalogMetadataAPI): true,
						})).NotTo(HaveOccurred())

						// reconcile
						res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
						Expect(res).To(Equal(ctrl.Result{}))
						Expect(err).ToNot(HaveOccurred())
					})

					AfterEach(func() {
						// clean up catalogmetadata
						Expect(cl.DeleteAllOf(ctx, &v1alpha1.CatalogMetadata{})).To(Succeed())
						Expect(features.CatalogdFeatureGate.SetFromMap(map[string]bool{
							string(features.CatalogMetadataAPI): false,
						})).NotTo(HaveOccurred())
					})

					// TODO (rashmigottipati): Add testing of CatalogMetadata sync process.
					It("should create CatalogMetadata resources", func() {
						catalogMetadatas := &v1alpha1.CatalogMetadataList{}
						Expect(cl.List(ctx, catalogMetadatas)).To(Succeed())
						Expect(catalogMetadatas.Items).To(HaveLen(3))
						for _, catalogMetadata := range catalogMetadatas.Items {
							Expect(catalogMetadata.Name).To(ContainSubstring(catalogKey.Name))
							Expect(catalogMetadata.Kind).To(Equal("CatalogMetadata"))
							Expect(catalogMetadata.OwnerReferences).To(HaveLen(1))
							Expect(catalogMetadata.OwnerReferences[0].Name).To(Equal(catalogKey.Name))
							Expect(catalogMetadata.Spec.Catalog.Name).To(Equal(catalogKey.Name))
						}
					})

					When("creating another Catalog", func() {
						var (
							tempCatalog *v1alpha1.Catalog
						)
						BeforeEach(func() {
							tempCatalog = &v1alpha1.Catalog{
								ObjectMeta: metav1.ObjectMeta{Name: "tempedout"},
								Spec: v1alpha1.CatalogSpec{
									Source: v1alpha1.CatalogSource{
										Type: "image",
										Image: &v1alpha1.ImageSource{
											Ref: "somecatalog:latest",
										},
									},
								},
							}

							Expect(cl.Create(ctx, tempCatalog)).To(Succeed())
							res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "tempedout"}})
							Expect(res).To(Equal(ctrl.Result{}))
							Expect(err).ToNot(HaveOccurred())
						})

						AfterEach(func() {
							Expect(cl.Delete(ctx, tempCatalog)).NotTo(HaveOccurred())
						})

						It("should not delete CatalogMetadata belonging to a different catalog", func() {
							catalogMetadatas := &v1alpha1.CatalogMetadataList{}
							Expect(cl.List(ctx, catalogMetadatas)).To(Succeed())
							Expect(catalogMetadatas.Items).To(HaveLen(6))
							for _, catalogMetadata := range catalogMetadatas.Items {
								for _, or := range catalogMetadata.GetOwnerReferences() {
									if or.Kind == "Catalog" {
										if or.Name == catalogKey.Name {
											Expect(catalogMetadata.Name).To(ContainSubstring(catalogKey.Name))
											Expect(catalogMetadata.Kind).To(Equal("CatalogMetadata"))
											Expect(catalogMetadata.Spec.Catalog.Name).To(Equal(catalogKey.Name))
											break
										} else if or.Name == tempCatalog.Name {
											Expect(catalogMetadata.Name).To(ContainSubstring(tempCatalog.Name))
											Expect(catalogMetadata.Kind).To(Equal("CatalogMetadata"))
											Expect(catalogMetadata.Spec.Catalog.Name).To(Equal(tempCatalog.Name))
											break
										}
									}
								}
							}
						})
					})
				})
			})
		})
	})
})

// The below string templates each represent a YAML file consisting
// of file-based catalog objects to build a minimal catalog consisting of
// one package, with one channel, and one bundle in that channel.
// To learn more about File-Based Catalogs and the different objects, view the
// documentation at https://olm.operatorframework.io/docs/reference/file-based-catalogs/.
// The reasoning behind having these as a template is to parameterize different
// fields to use custom values during testing and verifying to ensure that the BundleMetadata
// and Package resources created by the Catalog controller have the appropriate values.
// Having the parameterized fields allows us to easily change the values that are used in
// the tests by changing them in one place as opposed to manually changing many string literals
// throughout the code.
const testBundleTemplate = `---
image: %s
name: %s
schema: olm.bundle
package: %s
relatedImages:
  - name: %s
    image: %s
properties:
  - type: olm.bundle.object
    value:
      data: %s
  - type: some.other
    value:
      data: arbitrary-info
`

const testPackageTemplate = `---
defaultChannel: %s
name: %s
schema: olm.package
`

const testChannelTemplate = `---
schema: olm.channel
package: %s
name: %s
entries:
  - name: %s
`
