package controllers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/entity_sources/catalogsource"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultCatalogSourceSyncInterval     = 5 * time.Minute
	defaultRegistryGRPCConnectionTimeout = 10 * time.Second

	eventTypeNormal  = "Normal"
	eventTypeWarning = "Warning"

	eventReasonCacheUpdated      = "BundleCacheUpdated"
	eventReasonCacheUpdateFailed = "BundleCacheUpdateFailed"
)

type CatalogSourceReconcilerOption func(reconciler *CatalogSourceReconciler)

func WithRegistryClient(registry catalogsource.RegistryClient) CatalogSourceReconcilerOption {
	return func(reconciler *CatalogSourceReconciler) {
		reconciler.registry = registry
	}
}

// applyDefaults applies default values to empty CatalogSourceReconciler fields _after_ options have been applied
func applyDefaults() CatalogSourceReconcilerOption {
	return func(reconciler *CatalogSourceReconciler) {
		if reconciler.registry == nil {
			reconciler.registry = catalogsource.NewRegistryGRPCClient(defaultRegistryGRPCConnectionTimeout)
		}
	}
}

type CatalogSourceReconciler struct {
	sync.RWMutex
	client.Client
	scheme   *runtime.Scheme
	registry catalogsource.RegistryClient
	recorder record.EventRecorder
	cache    map[string]map[deppy.Identifier]*input.Entity
}

func NewCatalogSourceReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, options ...CatalogSourceReconcilerOption) *CatalogSourceReconciler {
	reconciler := &CatalogSourceReconciler{
		RWMutex:  sync.RWMutex{},
		Client:   client,
		scheme:   scheme,
		recorder: recorder,
		cache:    map[string]map[deppy.Identifier]*input.Entity{},
	}
	// apply options
	options = append(options, applyDefaults())
	for _, option := range options {
		option(reconciler)
	}

	return reconciler
}

// +kubebuilder:rbac:groups=operators.coreos.com,resources=catalogsources,verbs=get;list;watch

func (r *CatalogSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("catalogsource-controller")
	l.V(1).Info("starting")
	defer l.V(1).Info("ending")

	var catalogSource = &v1alpha1.CatalogSource{}
	if err := r.Client.Get(ctx, req.NamespacedName, catalogSource); err != nil {
		if errors.IsNotFound(err) {
			r.dropSource(req.String())
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	entities, err := r.registry.ListEntities(ctx, catalogSource)
	if err != nil {
		r.recorder.Event(catalogSource, eventTypeWarning, eventReasonCacheUpdateFailed, fmt.Sprintf("Failed to update bundle cache from %s/%s: %v", catalogSource.GetNamespace(), catalogSource.GetName(), err))
		return ctrl.Result{Requeue: true}, err
	}
	r.updateCache(req.String(), entities)
	r.recorder.Event(catalogSource, eventTypeNormal, eventReasonCacheUpdated, fmt.Sprintf("Successfully updated bundle cache from %s/%s", catalogSource.GetNamespace(), catalogSource.GetName()))

	if isManagedCatalogSource(*catalogSource) {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: defaultCatalogSourceSyncInterval}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CatalogSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.CatalogSource{}).
		Complete(r)
}

// TODO: find better way to identify catalogSources unmanaged by olm
func isManagedCatalogSource(catalogSource v1alpha1.CatalogSource) bool {
	return len(catalogSource.Spec.Address) == 0
}

func (r *CatalogSourceReconciler) updateCache(sourceID string, entities []*input.Entity) {
	r.RWMutex.Lock()
	defer r.RWMutex.Unlock()
	newSourceCache := make(map[deppy.Identifier]*input.Entity)
	for _, entity := range entities {
		newSourceCache[entity.Identifier()] = entity
	}
	r.cache[sourceID] = newSourceCache
}

func (r *CatalogSourceReconciler) dropSource(sourceID string) {
	r.RWMutex.Lock()
	defer r.RWMutex.Unlock()
	delete(r.cache, sourceID)
}

func (r *CatalogSourceReconciler) Get(ctx context.Context, id deppy.Identifier) *input.Entity {
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

func (r *CatalogSourceReconciler) Filter(ctx context.Context, filter input.Predicate) (input.EntityList, error) {
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

func (r *CatalogSourceReconciler) GroupBy(ctx context.Context, fn input.GroupByFunction) (input.EntityListMap, error) {
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

func (r *CatalogSourceReconciler) Iterate(ctx context.Context, fn input.IteratorFunction) error {
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
