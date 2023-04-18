package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/operator-framework/operator-controller/internal/resolution"
	"github.com/operator-framework/operator-controller/internal/resolution/entity_sources/catalogsource"
)

const (
	defaultEntitySourceSyncInterval = 5 * time.Minute

	eventTypeNormal  = "Normal"
	eventTypeWarning = "Warning"

	eventReasonCacheUpdated      = "BundleCacheUpdated"
	eventReasonCacheUpdateFailed = "BundleCacheUpdateFailed"
)

type EntitySourceReconcilerOption func(reconciler *EntitySourceReconciler)

func WithEntitySourceConnector(entitysourceConnector resolution.EntitySourceConnector) EntitySourceReconcilerOption {
	return func(reconciler *EntitySourceReconciler) {
		reconciler.entitySourceConnector = entitysourceConnector
	}
}

func WithSyncInterval(interval time.Duration) EntitySourceReconcilerOption {
	return func(reconciler *EntitySourceReconciler) {
		reconciler.syncInterval = interval
	}
}

// applyDefaults applies default values to empty EntitySourceReconciler fields _after_ options have been applied
func applyDefaults() EntitySourceReconcilerOption {
	return func(reconciler *EntitySourceReconciler) {
		if reconciler.entitySourceConnector == nil {
			reconciler.entitySourceConnector = catalogsource.NewGRPCClientConnector(0)
		}
		if reconciler.syncInterval == 0 {
			reconciler.syncInterval = defaultEntitySourceSyncInterval
		}
	}
}

type EntitySourceReconciler struct {
	sync.RWMutex
	client.Client
	scheme                *runtime.Scheme
	entitySourceConnector resolution.EntitySourceConnector
	recorder              record.EventRecorder
	syncInterval          time.Duration
	cache                 map[string]map[deppy.Identifier]*input.Entity
}

func NewEntitySourceReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, options ...EntitySourceReconcilerOption) *EntitySourceReconciler {
	reconciler := &EntitySourceReconciler{
		RWMutex:      sync.RWMutex{},
		Client:       client,
		scheme:       scheme,
		recorder:     recorder,
		syncInterval: 0,
		cache:        map[string]map[deppy.Identifier]*input.Entity{},
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

func (r *EntitySourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("entitysource-reconciler")
	l.V(1).Info("starting")
	defer l.V(1).Info("ending")

	var catalogSource = &v1alpha1.CatalogSource{}
	if err := r.Client.Get(ctx, req.NamespacedName, catalogSource); err != nil {
		if errors.IsNotFound(err) {
			r.dropSource(req.String())
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
	if updated := r.updateCache(req.String(), entities); updated {
		r.recorder.Event(catalogSource, eventTypeNormal, eventReasonCacheUpdated, fmt.Sprintf("Successfully updated bundle cache from %s/%s", catalogSource.GetNamespace(), catalogSource.GetName()))
	}

	// if isManagedCatalogSource(*catalogSource) {
	// 	return ctrl.Result{}, nil
	// }
	return ctrl.Result{RequeueAfter: r.syncInterval}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EntitySourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.CatalogSource{}).
		Complete(r)
}

// TODO: find better way to identify catalogSources unmanaged by olm
// func isManagedCatalogSource(catalogSource v1alpha1.CatalogSource) bool {
// 	return len(catalogSource.Spec.Address) == 0
// }

func (r *EntitySourceReconciler) updateCache(sourceID string, entities []*input.Entity) bool {
	newSourceCache := make(map[deppy.Identifier]*input.Entity)
	for _, entity := range entities {
		newSourceCache[entity.Identifier()] = entity
	}
	if _, ok := r.cache[sourceID]; ok && reflect.DeepEqual(r.cache[sourceID], newSourceCache) {
		return false
	}
	r.RWMutex.Lock()
	defer r.RWMutex.Unlock()
	r.cache[sourceID] = newSourceCache
	// return whether cache had updates
	return true
}

func (r *EntitySourceReconciler) dropSource(sourceID string) {
	r.RWMutex.Lock()
	defer r.RWMutex.Unlock()
	delete(r.cache, sourceID)
}

func (r *EntitySourceReconciler) Get(ctx context.Context, id deppy.Identifier) *input.Entity {
	r.RWMutex.RLock()
	defer r.RWMutex.RUnlock()
	// don't count on deppy ID to reflect its catalogsource
	for _, source := range r.cache {
		if entity, ok := source[id]; ok {
			return entity
		}
	}
	return nil
}

func (r *EntitySourceReconciler) Filter(ctx context.Context, filter input.Predicate) (input.EntityList, error) {
	resultSet := input.EntityList{}
	if err := r.Iterate(ctx, func(entity *input.Entity) error {
		if filter(entity) {
			resultSet = append(resultSet, *entity)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return resultSet, nil
}

func (r *EntitySourceReconciler) GroupBy(ctx context.Context, fn input.GroupByFunction) (input.EntityListMap, error) {
	resultSet := input.EntityListMap{}
	if err := r.Iterate(ctx, func(entity *input.Entity) error {
		keys := fn(entity)
		for _, key := range keys {
			resultSet[key] = append(resultSet[key], *entity)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return resultSet, nil
}

func (r *EntitySourceReconciler) Iterate(ctx context.Context, fn input.IteratorFunction) error {
	r.RWMutex.RLock()
	defer r.RWMutex.RUnlock()
	for _, source := range r.cache {
		for _, entity := range source {
			if err := fn(entity); err != nil {
				return err
			}
		}
	}
	return nil
}
