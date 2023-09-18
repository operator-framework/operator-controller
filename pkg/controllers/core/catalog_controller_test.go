package core_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
	"github.com/operator-framework/catalogd/pkg/storage"
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

var _ storage.Instance = &MockStore{}

type MockStore struct {
	shouldError bool
}

func (m MockStore) Store(_ string, _ fs.FS) error {
	if m.shouldError {
		return errors.New("mockstore store error")
	}
	return nil
}

func (m MockStore) Delete(_ string) error {
	if m.shouldError {
		return errors.New("mockstore delete error")
	}
	return nil
}

func (m MockStore) ContentURL(_ string) string {
	return "URL"
}

func (m MockStore) StorageServerHandler() http.Handler {
	panic("not needed")
}

var _ = Describe("Catalogd Controller Test", func() {
	format.MaxLength = 0
	var (
		ctx        context.Context
		reconciler *core.CatalogReconciler
		mockSource *MockSource
		mockStore  *MockStore
	)
	BeforeEach(func() {
		ctx = context.Background()
		mockSource = &MockSource{}
		mockStore = &MockStore{}
		reconciler = &core.CatalogReconciler{
			Client: cl,
			Unpacker: source.NewUnpacker(
				map[v1alpha1.SourceType]source.Unpacker{
					v1alpha1.SourceTypeImage: mockSource,
				},
			),
			Storage: mockStore,
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
				Expect(client.IgnoreNotFound(cl.Delete(ctx, catalog))).To(Succeed())
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
					testBundleName              = "webhook_operator.v0.0.1"
					testBundleImage             = "quay.io/olmtest/webhook-operator-bundle:0.0.3"
					testBundleRelatedImageName  = "test"
					testBundleRelatedImageImage = "testimage:latest"
					testBundleObjectData        = "dW5pbXBvcnRhbnQK"
					testPackageDefaultChannel   = "preview_test"
					testPackageName             = "webhook_operator_test"
					testChannelName             = "preview_test"
					testPackage                 = fmt.Sprintf(testPackageTemplate, testPackageDefaultChannel, testPackageName)
					testBundle                  = fmt.Sprintf(testBundleTemplate, testBundleImage, testBundleName, testPackageName, testBundleRelatedImageName, testBundleRelatedImageImage, testBundleObjectData)
					testChannel                 = fmt.Sprintf(testChannelTemplate, testPackageName, testChannelName, testBundleName)
				)
				BeforeEach(func() {

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

				When("HTTPServer feature gate is enabled", func() {
					BeforeEach(func() {
						Expect(features.CatalogdFeatureGate.SetFromMap(map[string]bool{
							string(features.HTTPServer): true,
						})).NotTo(HaveOccurred())
						// call reconciler so that initial finalizer setup is done here
						res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
						Expect(res).To(Equal(ctrl.Result{}))
						Expect(err).ToNot(HaveOccurred())
					})
					When("there is no error in storing the fbc", func() {
						BeforeEach(func() {
							By("setting up mockStore to return no error", func() {
								mockStore.shouldError = false
							})
							res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
							Expect(res).To(Equal(ctrl.Result{}))
							Expect(err).ToNot(HaveOccurred())
						})
						It("should reflect in the status condition", func() {
							cat := &v1alpha1.Catalog{}
							Expect(cl.Get(ctx, catalogKey, cat)).To(Succeed())
							Expect(cat.Status.Phase).To(Equal(v1alpha1.PhaseUnpacked))
							diff := cmp.Diff(meta.FindStatusCondition(cat.Status.Conditions, v1alpha1.TypeUnpacked), &metav1.Condition{
								Reason: v1alpha1.ReasonUnpackSuccessful,
								Status: metav1.ConditionTrue,
							}, cmpopts.IgnoreFields(metav1.Condition{}, "Type", "ObservedGeneration", "LastTransitionTime", "Message"))
							Expect(diff).To(Equal(""))
						})

						When("the catalog is deleted but there is an error deleting the stored FBC", func() {
							BeforeEach(func() {
								By("setting up mockStore to return an error", func() {
									mockStore.shouldError = true
								})
								Expect(cl.Delete(ctx, catalog)).To(Succeed())
								// call reconciler so that MockStore can send an error on deletion attempt
								res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
								Expect(res).To(Equal(ctrl.Result{}))
								Expect(err).To(HaveOccurred())
							})
							It("should set status condition to reflect the error", func() {
								// get the catalog and ensure status is set properly
								cat := &v1alpha1.Catalog{}
								Expect(cl.Get(ctx, catalogKey, cat)).To(Succeed())
								cond := meta.FindStatusCondition(cat.Status.Conditions, v1alpha1.TypeDelete)
								Expect(cond).To(Not(BeNil()))
								Expect(cond.Reason).To(Equal(v1alpha1.ReasonStorageDeleteFailed))
								Expect(cond.Status).To(Equal(metav1.ConditionFalse))
							})
						})
					})

					When("there is an error storing the fbc", func() {
						BeforeEach(func() {
							By("setting up mockStore to return an error", func() {
								mockStore.shouldError = true
							})
							res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
							Expect(res).To(Equal(ctrl.Result{}))
							Expect(err).To(HaveOccurred())
						})
						It("should set status condition to reflect that storage error", func() {
							cat := &v1alpha1.Catalog{}
							Expect(cl.Get(ctx, catalogKey, cat)).To(Succeed())
							Expect(cat.Status.ResolvedSource).To(BeNil())
							Expect(cat.Status.Phase).To(Equal(v1alpha1.PhaseFailing))
							diff := cmp.Diff(meta.FindStatusCondition(cat.Status.Conditions, v1alpha1.TypeUnpacked), &metav1.Condition{
								Reason: v1alpha1.ReasonStorageFailed,
								Status: metav1.ConditionFalse,
							}, cmpopts.IgnoreFields(metav1.Condition{}, "Type", "ObservedGeneration", "LastTransitionTime", "Message"))
							Expect(diff).To(Equal(""))
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
// fields to use custom values during testing and verifying to ensure that the catalog
// metadata served after the Catalog resource has been reconciled have the appropriate values.
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
