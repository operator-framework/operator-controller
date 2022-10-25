package controllers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/timflannagan/platform-operators/internal/clusteroperator"
)

var _ = Describe("Aggregated ClusterOperator Controller", func() {
	var (
		r *AggregatedClusterOperatorReconciler
	)
	BeforeEach(func() {
		r = &AggregatedClusterOperatorReconciler{
			Client: c,
		}
	})
	It("should successfully reconcile when no platformoperators exist on the cluster", func() {
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: clusteroperator.AggregateResourceName,
			},
		})
		Expect(err).ToNot(HaveOccurred())
	})
})
