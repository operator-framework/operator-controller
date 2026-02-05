package webhook

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// ClusterCatalog wraps the external v1.ClusterCatalog type and implements admission.Defaulter
type ClusterCatalog struct{}

// Default is the method that will be called by the webhook to apply defaults.
// Type-safe method signature - no runtime.Object or type assertion needed.
func (r *ClusterCatalog) Default(ctx context.Context, obj *ocv1.ClusterCatalog) error {
	log := log.FromContext(ctx)
	log.Info("Invoking Default method for ClusterCatalog", "object", obj)

	// Defaulting logic: add the "olm.operatorframework.io/metadata.name" label
	if obj.Labels == nil {
		obj.Labels = map[string]string{}
	}
	obj.Labels[ocv1.MetadataNameLabel] = obj.GetName()
	log.Info("default", ocv1.MetadataNameLabel, obj.Name, "labels", obj.Labels)

	return nil
}

// SetupWebhookWithManager sets up the webhook with the manager
func (r *ClusterCatalog) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &ocv1.ClusterCatalog{}).
		WithDefaulter(r).
		Complete()
}
