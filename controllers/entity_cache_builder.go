package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/operator-framework/operator-controller/internal/entitycache"
	"github.com/operator-framework/operator-controller/internal/resolution"
	"github.com/operator-framework/operator-controller/internal/resolution/entity_sources/catalogsource"
)

const (
	defaultSyncInterval = 5 * time.Minute

	eventTypeNormal  = "Normal"
	eventTypeWarning = "Warning"

	eventReasonCacheUpdated      = "CacheUpdated"
	eventReasonCacheUpdateFailed = "CacheUpdateFailed"
)

type EntityCacheBuilderOption func(reconciler *EntityCacheBuilder)

func WithEntitySourceConnector(entitysourceConnector resolution.EntitySourceConnector) EntityCacheBuilderOption {
	return func(reconciler *EntityCacheBuilder) {
		reconciler.entitySourceConnector = entitysourceConnector
	}
}

func WithSyncInterval(interval time.Duration) EntityCacheBuilderOption {
	return func(reconciler *EntityCacheBuilder) {
		reconciler.syncInterval = interval
	}
}

// applyDefaults applies default values to empty EntityCacheBuilder fields _after_ options have been applied
func applyDefaults() EntityCacheBuilderOption {
	return func(reconciler *EntityCacheBuilder) {
		if reconciler.entitySourceConnector == nil {
			reconciler.entitySourceConnector = catalogsource.NewGRPCClientConnector(0)
		}
		if reconciler.syncInterval == 0 {
			reconciler.syncInterval = defaultSyncInterval
		}
	}
}

type EntityCacheBuilder struct {
	client.Client
	scheme                *runtime.Scheme
	entitySourceConnector resolution.EntitySourceConnector
	recorder              record.EventRecorder
	syncInterval          time.Duration
	Cache                 *entitycache.EntityCache
}

func NewEntityCacheBuilder(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, options ...EntityCacheBuilderOption) *EntityCacheBuilder {
	reconciler := &EntityCacheBuilder{
		Client:       client,
		scheme:       scheme,
		recorder:     recorder,
		syncInterval: 0,
	}
	// apply options
	options = append(options, applyDefaults())
	for _, option := range options {
		option(reconciler)
	}

	return reconciler
}

// +kubebuilder:rbac:groups=operators.coreos.com,resources=catalogsources,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *EntityCacheBuilder) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("entity-cache-builder")
	l.V(1).Info("starting")
	defer l.V(1).Info("ending")

	var catalogSource = &v1alpha1.CatalogSource{}
	if err := r.Client.Get(ctx, req.NamespacedName, catalogSource); err != nil {
		if errors.IsNotFound(err) {
			r.Cache.DropSource(req.String())
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	entities, err := r.entitySourceConnector.ListEntities(ctx, catalogSource)
	// TODO: invalidate stale cache for failed updates
	if err != nil {
		r.recorder.Event(catalogSource, eventTypeWarning, eventReasonCacheUpdateFailed, fmt.Sprintf("Failed to update bundle cache from %s/%s: %v", catalogSource.GetNamespace(), catalogSource.GetName(), err))
		// return ctrl.Result{Requeue: !isManagedCatalogSource(*catalogSource)}, err
		return ctrl.Result{Requeue: true}, err
	}
	if updated := r.Cache.UpdateCache(req.String(), entities); updated {
		r.recorder.Event(catalogSource, eventTypeNormal, eventReasonCacheUpdated, fmt.Sprintf("Successfully updated bundle cache from %s/%s", catalogSource.GetNamespace(), catalogSource.GetName()))
	}

	// if isManagedCatalogSource(*catalogSource) {
	// 	return ctrl.Result{}, nil
	// }
	return ctrl.Result{RequeueAfter: r.syncInterval}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EntityCacheBuilder) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.CatalogSource{}).
		Complete(r)
}

// TODO: find better way to identify catalogSources unmanaged by olm
// func isManagedCatalogSource(catalogSource v1alpha1.CatalogSource) bool {
// 	return len(catalogSource.Spec.Address) == 0
// }
