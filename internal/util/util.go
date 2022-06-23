package util

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	platformv1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
)

var (
	ShortRequeue = ctrl.Result{RequeueAfter: time.Second * 5}
)

func RequeuePlatformOperators(cl client.Client) handler.MapFunc {
	return func(object client.Object) []reconcile.Request {
		poList := &platformv1alpha1.PlatformOperatorList{}
		if err := cl.List(context.Background(), poList); err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, po := range poList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: po.GetName(),
				},
			})
		}
		return requests
	}
}

func RequeueBundleInstance(c client.Client) handler.MapFunc {
	return func(obj client.Object) []reconcile.Request {
		bi := obj.(*rukpakv1alpha1.BundleInstance)

		poList := &platformv1alpha1.PlatformOperatorList{}
		if err := c.List(context.Background(), poList); err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, po := range poList.Items {
			po := po

			for _, ref := range bi.GetOwnerReferences() {
				if ref.Name == po.GetName() {
					requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&po)})
				}
			}
		}
		return requests
	}
}
