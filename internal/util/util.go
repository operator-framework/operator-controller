package util

import (
	"context"
	"os"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformtypes "github.com/operator-framework/operator-controller/api/v1alpha1"
)

// GetPodNamespace checks whether the controller is running in a Pod vs.
// being run locally by inspecting the namespace file that gets mounted
// automatically for Pods at runtime. If that file doesn't exist, then
// return the value of the defaultNamespace parameter.
func PodNamespace(defaultNamespace string) string {
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return defaultNamespace
	}
	return string(namespace)
}

func RequeueOperators(cl client.Client) handler.MapFunc {
	return func(object client.Object) []reconcile.Request {
		operators := &platformtypes.OperatorList{}
		if err := cl.List(context.Background(), operators); err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, o := range operators.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: o.GetName(),
				},
			})
		}
		return requests
	}
}

func RequeueBundleDeployment(c client.Client) handler.MapFunc {
	return func(obj client.Object) []reconcile.Request {
		bd := obj.(*rukpakv1alpha1.BundleDeployment)

		operators := &platformtypes.OperatorList{}
		if err := c.List(context.Background(), operators); err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, o := range operators.Items {
			o := o

			for _, ref := range bd.GetOwnerReferences() {
				if ref.Name == o.GetName() {
					requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&o)})
				}
			}
		}
		return requests
	}
}

// InspectBundleDeployment is responsible for inspecting an individual BD
// resource, and verifying whether the referenced Bundle contents has been
// successfully unpacked and persisted to the cluster. In the case that the
// BD is reporting a successful status, a nil metav1.Condition will be returned.
func InspectBundleDeployment(_ context.Context, conditions []metav1.Condition) *metav1.Condition {
	unpacked := meta.FindStatusCondition(conditions, rukpakv1alpha1.TypeHasValidBundle)
	if unpacked == nil {
		return &metav1.Condition{
			Type:    platformtypes.TypeInstalled,
			Status:  metav1.ConditionFalse,
			Reason:  platformtypes.ReasonUnpackPending,
			Message: "Waiting for the bundle to be unpacked",
		}
	}
	if unpacked.Status != metav1.ConditionTrue {
		return &metav1.Condition{
			Type:    platformtypes.TypeInstalled,
			Status:  metav1.ConditionFalse,
			Reason:  unpacked.Reason,
			Message: unpacked.Message,
		}
	}

	installed := meta.FindStatusCondition(conditions, rukpakv1alpha1.TypeInstalled)
	if installed == nil {
		return &metav1.Condition{
			Type:    platformtypes.TypeInstalled,
			Status:  metav1.ConditionFalse,
			Reason:  platformtypes.ReasonInstallPending,
			Message: "Waiting for the bundle to be installed",
		}
	}
	if installed.Status != metav1.ConditionTrue {
		return &metav1.Condition{
			Type:    platformtypes.TypeInstalled,
			Status:  metav1.ConditionFalse,
			Reason:  installed.Reason,
			Message: installed.Message,
		}
	}
	return nil
}
