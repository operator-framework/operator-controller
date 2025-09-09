package webhook

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// ClusterCatalog wraps the external v1.ClusterCatalog type and implements admission.Defaulter
type ClusterCatalog struct{}

// Default is the method that will be called by the webhook to apply defaults.
func (r *ClusterCatalog) Default(ctx context.Context, obj runtime.Object) error {
	log := log.FromContext(ctx)
	log.Info("Invoking Default method for ClusterCatalog", "object", obj)
	catalog, ok := obj.(*ocv1.ClusterCatalog)
	if !ok {
		return fmt.Errorf("expected a ClusterCatalog but got a %T", obj)
	}

	// Defaulting logic: add the "olm.operatorframework.io/metadata.name" label
	if catalog.Labels == nil {
		catalog.Labels = map[string]string{}
	}
	catalog.Labels[ocv1.MetadataNameLabel] = catalog.GetName()
	log.Info("default", ocv1.MetadataNameLabel, catalog.Name, "labels", catalog.Labels)

	return nil
}

// SetupWebhookWithManager sets up the webhook with the manager
func (r *ClusterCatalog) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&ocv1.ClusterCatalog{}).
		WithDefaulter(r).
		Complete()
}
