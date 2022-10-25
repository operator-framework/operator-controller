package util

import (
	"context"
	"fmt"
	"os"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerror "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/openshift/api/platform/v1alpha1"
	platformtypes "github.com/timflannagan/platform-operators/api/v1alpha1"
)

const (
	// This is the error message thrown by ServerSupportsVersion function
	// when an API version is not supported by the server.
	notSupportedErrorMessage = "server does not support API version"
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

func RequeueBundleDeployment(c client.Client) handler.MapFunc {
	return func(obj client.Object) []reconcile.Request {
		bi := obj.(*rukpakv1alpha1.BundleDeployment)

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

func RequeueClusterOperator(c client.Client, name string) handler.MapFunc {
	return func(obj client.Object) []reconcile.Request {
		co := &configv1.ClusterOperator{}

		if err := c.Get(context.Background(), types.NamespacedName{Name: name}, co); err != nil {
			return nil
		}
		return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(co)}}
	}
}

// InspectPlatformOperators iterates over all the POs in the list
// and determines whether a PO is in a failing state by inspecting its status.
// A nil return value indicates no errors were found with the POs provided.
func InspectPlatformOperators(poList *platformv1alpha1.PlatformOperatorList) error {
	var poErrors []error
	for _, po := range poList.Items {
		if err := inspectPlatformOperator(po); err != nil {
			poErrors = append(poErrors, err)
		}
	}
	return utilerror.NewAggregate(poErrors)
}

// inspectPlatformOperator is responsible for inspecting an individual platform
// operator resource, and determining whether it's reporting any failing conditions.
// In the case that the PO resource is expressing failing states, then an error
// will be returned to reflect that.
func inspectPlatformOperator(po platformv1alpha1.PlatformOperator) error {
	installed := meta.FindStatusCondition(po.Status.Conditions, platformtypes.TypeInstalled)
	if installed == nil {
		return buildPOFailureMessage(po.GetName(), platformtypes.ReasonInstallPending)
	}
	if installed.Status != metav1.ConditionTrue {
		return buildPOFailureMessage(po.GetName(), installed.Reason)
	}
	return nil
}

func buildPOFailureMessage(name, reason string) error {
	return fmt.Errorf("encountered the failing %s platform operator with reason %q", name, reason)
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

func IsAPIAvailable(c discovery.DiscoveryInterface, gv schema.GroupVersion) (bool, error) {
	discoveryErr := discovery.ServerSupportsVersion(c, gv)
	if strings.Contains(discoveryErr.Error(), notSupportedErrorMessage) {
		return false, nil
	}
	if discoveryErr != nil {
		return false, discoveryErr
	}
	return true, nil
}
