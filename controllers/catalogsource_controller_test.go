package controllers_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	catalogsource2 "github.com/operator-framework/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/resolution/entity_sources/catalogsource"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ catalogsource.RegistryClient = &fakeRegistryClient{}

type catalogContents struct {
	Entities []*input.Entity
	err      error
}

type fakeRegistryClient struct {
	catalogSource map[string]catalogContents
}

func (r *fakeRegistryClient) setEntitiesForSource(catalogSourceID string, entities ...*input.Entity) {
	if r.catalogSource == nil {
		r.catalogSource = map[string]catalogContents{}
	}
	r.catalogSource[catalogSourceID] = catalogContents{
		Entities: entities,
	}
}

func (r *fakeRegistryClient) setErrorForSource(catalogSourceID string, err error) {
	r.catalogSource[catalogSourceID] = catalogContents{
		err: err,
	}
}

func (r *fakeRegistryClient) ListEntities(ctx context.Context, catsrc *v1alpha1.CatalogSource) ([]*input.Entity, error) {
	catalogSourceKey := types.NamespacedName{Namespace: catsrc.Namespace, Name: catsrc.Name}.String()
	if src, ok := r.catalogSource[catalogSourceKey]; ok {
		return src.Entities, src.err
	}
	return []*input.Entity{}, nil
}

var _ = Describe("CatalogSource Controller Test", func() {
	var (
		ctx          context.Context
		reconciler   *catalogsource2.CatalogSourceReconciler
		fakeRecorder record.EventRecorder
		fakeRegistry *fakeRegistryClient
	)
	BeforeEach(func() {
		ctx = context.Background()
		fakeRecorder = record.NewFakeRecorder(5)
		fakeRegistry = &fakeRegistryClient{}
		reconciler = catalogsource2.NewCatalogSourceReconciler(
			cl,
			sch,
			fakeRecorder,
			catalogsource2.WithRegistryClient(fakeRegistry),
		)
	})
	When("the catalog source does not exist", func() {
		It("returns no error", func() {
			res, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "non-existent", Namespace: "some-namespace"}})
			Expect(res).To(Equal(ctrl.Result{}))
			Expect(err).NotTo(HaveOccurred())
		})
	})
	When("the catalog source exists", func() {
		var (
			catalogSource     *v1alpha1.CatalogSource
			opKey             types.NamespacedName
			catalogSourceName string
			namespace         string
		)
		BeforeEach(func() {
			catalogSourceName = fmt.Sprintf("catalogsource-test-%s", rand.String(8))
			namespace = fmt.Sprintf("test-namespace-%s", rand.String(8))
			Expect(cl.Create(ctx, &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			})).To(Succeed())
			opKey = types.NamespacedName{Name: catalogSourceName, Namespace: namespace}

			By("initializing cluster state")
			catalogSource = &v1alpha1.CatalogSource{
				ObjectMeta: metav1.ObjectMeta{Name: catalogSourceName, Namespace: namespace},
			}
			err := cl.Create(ctx, catalogSource)
			Expect(err).NotTo(HaveOccurred())
		})
		It("keeps manages the cache", func() {
			By("running reconcile")
			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
			Expect(res).To(Equal(ctrl.Result{}))

			By("checking the cache is empty")
			var entities []*input.Entity
			err = reconciler.Iterate(ctx, func(entity *input.Entity) error {
				entities = append(entities, entity)
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(entities).To(BeEmpty())

			By("updating the catalog source contents")
			registryEntities := []*input.Entity{
				input.NewEntity(deppy.Identifier(fmt.Sprintf("%s/%s/pkg1/chan1/0.1.0", catalogSourceName, namespace)), map[string]string{}),
			}
			fakeRegistry.setEntitiesForSource(opKey.String(), registryEntities...)

			By("running re-reconcile")
			res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
			Expect(res).To(Equal(ctrl.Result{}))

			By("checking the cache is populated")
			entities = nil
			err = reconciler.Iterate(ctx, func(entity *input.Entity) error {
				entities = append(entities, entity)
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(entities).To(Equal(registryEntities))

			By("deleting the catalog source")
			Expect(cl.Delete(ctx, catalogSource)).To(Succeed())

			By("running re-reconcile")
			res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
			Expect(res).To(Equal(ctrl.Result{}))
			Expect(err).ToNot(HaveOccurred())

			By("checking the cache is empty again")
			entities = nil
			err = reconciler.Iterate(ctx, func(entity *input.Entity) error {
				entities = append(entities, entity)
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(entities).To(BeEmpty())
		})

		Describe("querying CatalogSource EntitySource", func() {
			BeforeEach(func() {
				registryEntities := []*input.Entity{
					input.NewEntity(deppy.Identifier(fmt.Sprintf("%s/%s/pkg1/chan1/0.1.0", catalogSourceName, namespace)), map[string]string{}),
					input.NewEntity(deppy.Identifier(fmt.Sprintf("%s/%s/pkg1/chan1/0.2.0", catalogSourceName, namespace)), map[string]string{"k": "v"}),
				}
				fakeRegistry.setEntitiesForSource(opKey.String(), registryEntities...)

				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(err).ToNot(HaveOccurred())
				Expect(res).To(Equal(ctrl.Result{}))
			})
			Describe("Get", func() {
				It("should fetch an entity by ID", func() {
					Expect(reconciler.Get(ctx, deppy.Identifier(fmt.Sprintf("%s/%s/pkg1/chan1/0.1.0", catalogSourceName, namespace)))).To(
						Equal(input.NewEntity(deppy.Identifier(fmt.Sprintf("%s/%s/pkg1/chan1/0.1.0", catalogSourceName, namespace)), map[string]string{})),
					)
				})
				It("should not fetch anything for nonexistent entity ID", func() {
					Expect(reconciler.Get(ctx, "non-existent")).To(BeNil())
				})
			})
			Describe("Filter", func() {
				It("should return entities that meet filter predicates", func() {
					actual, err := reconciler.Filter(ctx, func(e *input.Entity) bool {
						_, ok := e.Properties["k"]
						return ok
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(actual).To(ConsistOf(input.EntityList{*input.NewEntity(deppy.Identifier(fmt.Sprintf("%s/%s/pkg1/chan1/0.2.0", catalogSourceName, namespace)), map[string]string{"k": "v"})}))
				})
			})
			Describe("GroupBy", func() {
				It("should group entities by the keys provided by the groupBy function", func() {
					actual, err := reconciler.GroupBy(ctx, func(e *input.Entity) []string {
						var keys []string
						for k := range e.Properties {
							keys = append(keys, k)
						}
						return keys
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(actual).To(Equal(input.EntityListMap{"k": input.EntityList{*input.NewEntity(deppy.Identifier(fmt.Sprintf("%s/%s/pkg1/chan1/0.2.0", catalogSourceName, namespace)), map[string]string{"k": "v"})}}))
				})
			})
			Describe("Iterate", func() {
				It("should go through all entities", func() {
					var ids []string
					Expect(reconciler.Iterate(ctx, func(e *input.Entity) error {
						ids = append(ids, e.Identifier().String())
						return nil
					})).To(BeNil())
					Expect(ids).To(ConsistOf([]string{fmt.Sprintf("%s/%s/pkg1/chan1/0.1.0", catalogSourceName, namespace),
						fmt.Sprintf("%s/%s/pkg1/chan1/0.2.0", catalogSourceName, namespace)}))
				})
			})
		})
	})
})
