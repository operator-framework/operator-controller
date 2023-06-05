package controllers_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/controllers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func collectRequests(reqChan <-chan string) func() []string {
	var reqNames []string
	return func() []string {
		select {
		case req := <-reqChan:
			reqNames = append(reqNames, req)
		default:
		}
		return reqNames
	}
}

func operatorForPackage(name, pkg string) *operatorsv1alpha1.Operator {
	return &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: pkg,
		},
	}
}

var _ = Describe("SetupWithManager", func() {
	When("catalog events occur", func() {
		var cancel context.CancelFunc
		var ctx context.Context
		var opNames []string
		var reqChan chan string
		BeforeEach(func() {
			ctx = context.Background()
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: sch})
			Expect(err).To(BeNil())

			opNames = []string{"prometheus", "project-quay"}
			reqChan = make(chan string)
			var fakeReconciler reconcile.Func = func(_ context.Context, request ctrl.Request) (ctrl.Result, error) {
				reqChan <- request.Name
				return ctrl.Result{}, nil
			}
			Expect(controllers.SetupWithManager(fakeReconciler, mgr)).To(Succeed())

			var mgrCtx context.Context
			mgrCtx, cancel = context.WithCancel(ctx)
			go func() {
				Expect(mgr.Start(mgrCtx)).To(Succeed())
			}()

			for _, p := range opNames {
				Expect(cl.Create(ctx, operatorForPackage(p, p))).To(Succeed())
			}
			By("verifying initial reconcile logs for operator creation")
			Eventually(collectRequests(reqChan)).Should(ConsistOf(opNames))
		})
		It("reconciles all affected operators on cluster", func() {
			By("creating a new catalog")
			catalog := &catalogd.Catalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "t",
				},
				Spec: catalogd.CatalogSpec{
					Source: catalogd.CatalogSource{
						Type:  catalogd.SourceTypeImage,
						Image: &catalogd.ImageSource{},
					},
				},
			}
			Expect(cl.Create(ctx, catalog)).To(Succeed())
			By("verifying operator reconcile logs on catalog create")
			Eventually(collectRequests(reqChan)).Should(ConsistOf(opNames))

			By("updating a catalog")
			catalog.Spec.Source.Image.Ref = "s"
			Expect(cl.Update(ctx, catalog)).To(Succeed())
			By("verifying operator reconcile logs on catalog update")
			Eventually(collectRequests(reqChan)).Should(ConsistOf(opNames))

			By("deleting a catalog")
			Expect(cl.Delete(ctx, catalog)).To(Succeed())
			By("verifying operator reconcile logs on catalog delete")
			Eventually(collectRequests(reqChan)).Should(ConsistOf(opNames))
		})
		AfterEach(func() {
			cancel() // stop manager
			close(reqChan)
			for _, p := range opNames {
				Expect(cl.Delete(ctx, operatorForPackage(p, p))).To(Succeed())
			}
		})
	})
})
