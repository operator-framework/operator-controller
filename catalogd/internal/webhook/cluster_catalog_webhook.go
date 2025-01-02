package webhook

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	catalogdv1 "github.com/operator-framework/catalogd/api/v1"
)

// +kubebuilder:webhook:admissionReviewVersions={v1},failurePolicy=Fail,groups=olm.operatorframework.io,mutating=true,name=inject-metadata-name.olm.operatorframework.io,path=/mutate-olm-operatorframework-io-v1-clustercatalog,resources=clustercatalogs,verbs=create;update,versions=v1,sideEffects=None,timeoutSeconds=10

// +kubebuilder:rbac:groups=olm.operatorframework.io,resources=clustercatalogs,verbs=get;list;watch;patch;update

// ClusterCatalog wraps the external v1.ClusterCatalog type and implements admission.Defaulter
type ClusterCatalog struct{}

// Default is the method that will be called by the webhook to apply defaults.
func (r *ClusterCatalog) Default(ctx context.Context, obj runtime.Object) error {
	log := log.FromContext(ctx)
	log.Info("Invoking Default method for ClusterCatalog", "object", obj)
	catalog, ok := obj.(*catalogdv1.ClusterCatalog)
	if !ok {
		return fmt.Errorf("expected a ClusterCatalog but got a %T", obj)
	}

	// Defaulting logic: add the "olm.operatorframework.io/metadata.name" label
	if catalog.Labels == nil {
		catalog.Labels = map[string]string{}
	}
	catalog.Labels[catalogdv1.MetadataNameLabel] = catalog.GetName()
	log.Info("default", catalogdv1.MetadataNameLabel, catalog.Name, "labels", catalog.Labels)

	return nil
}

// SetupWebhookWithManager sets up the webhook with the manager
func (r *ClusterCatalog) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&catalogdv1.ClusterCatalog{}).
		WithDefaulter(r).
		Complete()
}
