package catalogsource_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity_sources/catalogsource"
	"github.com/operator-framework/operator-registry/alpha/property"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	crClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type catalogContents struct {
	Entities []*input.Entity
	err      error
}

type fakeRegistryClient struct {
	catalogSource map[string]catalogContents
}

func (r *fakeRegistryClient) ListEntities(ctx context.Context, catsrc *v1alpha1.CatalogSource) ([]*input.Entity, error) {
	catalogSourceKey := types.NamespacedName{Namespace: catsrc.Namespace, Name: catsrc.Name}.String()
	if src, ok := r.catalogSource[catalogSourceKey]; ok {
		return src.Entities, src.err
	}
	return []*input.Entity{}, nil
}

var _ = Describe("Registry EntitySource", func() {
	var querier *catalogsource.CachedRegistryEntitySource
	var registryClient *fakeRegistryClient
	var cli crClient.WithWatch
	var cacheSyncInterval = 5 * time.Second

	cachedEntityIDs := func() []deppy.Identifier {
		var entityIDs []deppy.Identifier
		err := querier.Iterate(context.TODO(), func(e *input.Entity) error {
			entityIDs = append(entityIDs, e.ID)
			return nil
		})
		Expect(err).To(BeNil())
		return entityIDs
	}

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		err := corev1.AddToScheme(scheme)
		Expect(err).To(BeNil())
		err = v1alpha1.AddToScheme(scheme)
		Expect(err).To(BeNil())
		cli = fake.NewClientBuilder().WithScheme(scheme).WithLists(&v1alpha1.CatalogSourceList{
			Items: []v1alpha1.CatalogSource{{
				ObjectMeta: metav1.ObjectMeta{Name: "hardcoded", Namespace: "test-ns"},
				Spec:       v1alpha1.CatalogSourceSpec{Address: "hardcoded/catsrc/addr"},
			}, {
				ObjectMeta: metav1.ObjectMeta{Name: "withsvc", Namespace: "test-ns"},
				Status:     v1alpha1.CatalogSourceStatus{RegistryServiceStatus: &v1alpha1.RegistryServiceStatus{ServiceNamespace: "test-ns", ServiceName: "test-svc", Port: "0"}},
			}},
		}).Build()

		registryClient = &fakeRegistryClient{
			catalogSource: map[string]catalogContents{
				"test-ns/withsvc": {
					Entities: []*input.Entity{
						{
							ID: "test-ns/withsvc/pkg1/chan1/0.1.0",
							Properties: map[string]string{
								"olm.gvk":        `[{"group":"foo","kind":"prov1","version":"v1"}]`,
								"olm.deprecated": `[{}]`,
							},
						},
					},
				},
				"test-ns/hardcoded": {
					Entities: []*input.Entity{
						{
							ID: "test-ns/hardcoded/pkg2/chan2/0.2.0",
							Properties: map[string]string{
								"olm.gvk": `[{"group":"foo","kind":"prov1","version":"v1"},{"group":"foo","kind":"prov2","version":"v1"}]`,
							},
						},
					},
				},
			},
		}

		querier = catalogsource.NewCachedRegistryQuerier(cli, catalogsource.WithRegistryClient(registryClient), catalogsource.WithSyncInterval(cacheSyncInterval))
		go func() {
			querier.Start(context.TODO())
		}()
		Eventually(func(g Gomega) {
			g.Expect(cachedEntityIDs()).To(ConsistOf([]deppy.Identifier{"test-ns/hardcoded/pkg2/chan2/0.2.0", "test-ns/withsvc/pkg1/chan1/0.1.0"}))
		}).Should(Succeed())
	})

	AfterEach(func() {
		querier.Stop()
	})

	Describe("CachedRegistryEntitySource", func() {
		When("initialized with existing catalogsources on cluster", func() {
			Describe("Get", func() {
				It("should fetch an entity by ID", func() {
					Eventually(func(g Gomega) {
						entity := querier.Get(context.TODO(), "test-ns/withsvc/pkg1/chan1/0.1.0")
						g.Expect(entity).To(Equal(&input.Entity{
							ID: "test-ns/withsvc/pkg1/chan1/0.1.0",
							Properties: map[string]string{
								"olm.gvk":        `[{"group":"foo","kind":"prov1","version":"v1"}]`,
								"olm.deprecated": `[{}]`,
							},
						}))
					}).Should(Succeed())
				})
			})

			Describe("Filter", func() {
				It("should return entities that meet filter predicates", func() {
					Eventually(func(g Gomega) {
						entityList, err := querier.Filter(context.TODO(), func(e *input.Entity) bool {
							_, deprecated := e.Properties["olm.deprecated"]
							return !deprecated
						})
						g.Expect(err).To(BeNil())
						g.Expect(entityList).To(Equal(input.EntityList{
							{
								ID: "test-ns/hardcoded/pkg2/chan2/0.2.0",
								Properties: map[string]string{
									"olm.gvk": `[{"group":"foo","kind":"prov1","version":"v1"},{"group":"foo","kind":"prov2","version":"v1"}]`,
								},
							},
						}))
					}).Should(Succeed())
				})
			})

			Describe("GroupBy", func() {
				It("should group entities by the keys provided by the groupBy function", func() {
					Eventually(func(g Gomega) {
						entityListMap, err := querier.GroupBy(context.TODO(), func(e *input.Entity) []string {
							var gvks []string
							if _, ok := e.Properties[property.TypeGVK]; ok {
								var props []property.GVK
								err := json.Unmarshal([]byte(e.Properties[property.TypeGVK]), &props)
								g.Expect(err).To(BeNil())
								for _, gvk := range props {
									key := fmt.Sprintf("%s/%s/%s", gvk.Group, gvk.Version, gvk.Kind)
									gvks = append(gvks, strings.Trim(key, "/"))
								}
							}
							return gvks
						})
						g.Expect(err).To(BeNil())

						g.Expect(entityListMap["foo/v1/prov1"]).To(Equal(input.EntityList{{
							ID: "test-ns/withsvc/pkg1/chan1/0.1.0",
							Properties: map[string]string{
								"olm.gvk":        `[{"group":"foo","kind":"prov1","version":"v1"}]`,
								"olm.deprecated": `[{}]`,
							},
						}, {
							ID: "test-ns/hardcoded/pkg2/chan2/0.2.0",
							Properties: map[string]string{
								"olm.gvk": `[{"group":"foo","kind":"prov1","version":"v1"},{"group":"foo","kind":"prov2","version":"v1"}]`,
							},
						}}))

						g.Expect(entityListMap["foo/v1/prov2"]).To(Equal(input.EntityList{{
							ID: "test-ns/hardcoded/pkg2/chan2/0.2.0",
							Properties: map[string]string{
								"olm.gvk": `[{"group":"foo","kind":"prov1","version":"v1"},{"group":"foo","kind":"prov2","version":"v1"}]`,
							},
						}}))
					}).Should(Succeed())
				})
			})

			Describe("Iterate", func() {
				It("should go through all entities", func() {
					Eventually(func(g Gomega) {
						var expectedIDs, entityIDs []deppy.Identifier
						for _, c := range registryClient.catalogSource {
							for _, e := range c.Entities {
								expectedIDs = append(expectedIDs, e.ID)
							}
						}
						err := querier.Iterate(context.TODO(), func(e *input.Entity) error {
							entityIDs = append(entityIDs, e.ID)
							return nil
						})
						g.Expect(err).To(BeNil())
						g.Expect(entityIDs).To(ConsistOf(expectedIDs))
					}).Should(Succeed())
				})
			})
		})

		When("catalogsource changes on cluster", func() {
			It("should update entity cache when new catalogsource is added", func() {
				addCatsrc := func(ctx context.Context, catsrc *v1alpha1.CatalogSource, obj []crClient.Object, contents catalogContents) {
					catalogSourceKey := types.NamespacedName{Namespace: catsrc.Namespace, Name: catsrc.Name}.String()
					registryClient.catalogSource[catalogSourceKey] = contents
					for _, o := range obj {
						// apply any manifests (pod, svc) for the catalogsource
						err := cli.Create(ctx, o)
						Expect(err).To(BeNil())
					}
					err := cli.Create(ctx, catsrc)
					Expect(err).To(BeNil())
				}

				addCatsrc(context.TODO(), &v1alpha1.CatalogSource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hardcoded-2",
						Namespace: "ns",
					},
					Spec: v1alpha1.CatalogSourceSpec{
						Address: "new/hardcoded/catsrc",
					},
				}, nil, catalogContents{
					Entities: []*input.Entity{
						{ID: "ns/hardcoded-2/pkg/channel/1.0.0"},
						{ID: "ns/hardcoded-2/pkg/channel/1.1.0"},
					},
				})

				Eventually(func(g Gomega) {
					g.Expect(cachedEntityIDs()).To(ConsistOf([]deppy.Identifier{
						"test-ns/hardcoded/pkg2/chan2/0.2.0",
						"test-ns/withsvc/pkg1/chan1/0.1.0",
						"ns/hardcoded-2/pkg/channel/1.0.0",
						"ns/hardcoded-2/pkg/channel/1.1.0",
					}))
				}).Should(Succeed())
			})

			It("should update entity cache when existing catalogsource changes", func() {
				modifyCatsrc := func(ctx context.Context, catalogSourceKey types.NamespacedName, catsrc *v1alpha1.CatalogSource, obj []crClient.Object, contents catalogContents) {
					registryClient.catalogSource[catalogSourceKey.String()] = contents
					for _, o := range obj {
						err := cli.Get(ctx, types.NamespacedName{Namespace: o.GetNamespace(), Name: o.GetName()}, &unstructured.Unstructured{})
						if errors.IsNotFound(err) {
							err := cli.Create(context.TODO(), o)
							Expect(err).To(BeNil())
							continue
						}
						Expect(err).To(BeNil())

						err = cli.Update(context.TODO(), o)
						Expect(err).To(BeNil())
					}
					if catsrc != nil {
						Eventually(func(g Gomega) {
							// client-side apply may fail, retry till successful
							err := cli.Update(context.TODO(), catsrc)

							g.Expect(err).To(BeNil())
						}).Should(Succeed())
					}
				}

				Eventually(func(g Gomega) {
					g.Expect(querier.Get(context.TODO(), "test-ns/hardcoded/pkg2/chan2/0.2.0")).To(Equal(&input.Entity{
						ID: "test-ns/hardcoded/pkg2/chan2/0.2.0",
						Properties: map[string]string{
							"olm.gvk": `[{"group":"foo","kind":"prov1","version":"v1"},{"group":"foo","kind":"prov2","version":"v1"}]`,
						},
					}))
				}).Should(Succeed())

				By("Updating a managed catalogSource", func() {
					catsrc := v1alpha1.CatalogSource{}
					err := cli.Get(context.TODO(), types.NamespacedName{Name: "hardcoded", Namespace: "test-ns"}, &catsrc)
					Expect(err).To(BeNil()) // catsrc should already exist, created as part of test setup
					catsrc.Spec.Address = "hardcoded/catsrc/addr"
					modifyCatsrc(context.TODO(), types.NamespacedName{Name: "hardcoded", Namespace: "test-ns"}, &catsrc, nil, catalogContents{
						Entities: []*input.Entity{
							{ID: "test-ns/hardcoded/pkg2/chan2/0.2.0"}, //overwrite
							{ID: "test-ns/hardcoded/pkg2/chan2/0.2.1"},
						},
					})
					Eventually(func(g Gomega) {
						g.Expect(cachedEntityIDs()).To(ConsistOf([]deppy.Identifier{
							"test-ns/hardcoded/pkg2/chan2/0.2.0",
							"test-ns/hardcoded/pkg2/chan2/0.2.1",
							"test-ns/withsvc/pkg1/chan1/0.1.0"}))

						// entity must be updated to reflect new value
						g.Expect(querier.Get(context.TODO(), "test-ns/hardcoded/pkg2/chan2/0.2.0")).To(Equal(&input.Entity{
							ID: "test-ns/hardcoded/pkg2/chan2/0.2.0",
						}))
					}).Should(Succeed())
				})

				By("Updating an unmanaged catalogSource", func() {
					modifyCatsrc(context.TODO(), types.NamespacedName{Name: "hardcoded", Namespace: "test-ns"}, nil, nil, catalogContents{
						Entities: []*input.Entity{
							{ID: "test-ns/hardcoded/pkg2/chan2/0.2.1", Properties: map[string]string{"foo": "bar"}},
						},
					})
					Eventually(func(g Gomega) {
						g.Expect(cachedEntityIDs()).To(ConsistOf([]deppy.Identifier{
							"test-ns/hardcoded/pkg2/chan2/0.2.1",
							"test-ns/withsvc/pkg1/chan1/0.1.0"}))

						// entity must be updated to reflect new value
						g.Expect(querier.Get(context.TODO(), "test-ns/hardcoded/pkg2/chan2/0.2.1")).To(Equal(&input.Entity{
							ID: "test-ns/hardcoded/pkg2/chan2/0.2.1", Properties: map[string]string{"foo": "bar"},
						}))
					}).WithPolling(time.Second).WithTimeout(2 * cacheSyncInterval).Should(Succeed())
				})
			})

			It("should purge cache of invalid entities when a catalogsource is deleted", func() {
				deleteCatsrc := func(ctx context.Context, catsrc *v1alpha1.CatalogSource, obj []crClient.Object) {
					catalogSourceKey := types.NamespacedName{Namespace: catsrc.Namespace, Name: catsrc.Name}.String()
					delete(registryClient.catalogSource, catalogSourceKey)
					for _, o := range obj {
						err := cli.Delete(ctx, o)
						Expect(err).To(BeNil())
					}
					err := cli.Delete(ctx, catsrc)
					Expect(err).To(BeNil())
				}
				deleteCatsrc(context.TODO(), &v1alpha1.CatalogSource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hardcoded",
						Namespace: "test-ns",
					},
					Spec: v1alpha1.CatalogSourceSpec{
						Address: "new/hardcoded/catsrc",
					},
				}, nil)

				Eventually(func(g Gomega) {
					g.Expect(cachedEntityIDs()).To(ConsistOf([]deppy.Identifier{"test-ns/withsvc/pkg1/chan1/0.1.0"}))
				}).Should(Succeed())
			})
		})
	})
})
