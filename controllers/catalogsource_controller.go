package controllers

import (
	"context"
	"sync"
	"time"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity_sources/catalogsource"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultCatalogSourceSyncInterval = 5 * time.Minute

type CatalogSourceReconciler struct {
	sync.RWMutex
	client.Client
	registry catalogsource.RegistryClient
	cache    map[string]sourceCache
}

type sourceCache struct {
	Items []*input.Entity
}

// +kubebuilder:rbac:groups=operators.coreos.com,resources=catalogsources,verbs=get;list;watch
func (r *CatalogSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var catalogSource = &v1alpha1.CatalogSource{}
	if err := r.Client.Get(ctx, req.NamespacedName, catalogSource); err != nil {
		if errors.IsNotFound(err) {
			delete(r.cache, req.String())
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	entities, err := r.registry.ListEntities(ctx, catalogSource)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	r.cache[req.String()] = sourceCache{Items: entities}

	if isManagedCatalogSource(*catalogSource) {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: defaultCatalogSourceSyncInterval}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CatalogSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.CatalogSource{}).
		Complete(r)

	if err != nil {
		return err
	}
	return nil
}

// TODO: find better way to identify catalogSources unmanaged by olm
func isManagedCatalogSource(catalogSource v1alpha1.CatalogSource) bool {
	return len(catalogSource.Spec.Address) == 0
}

func (r *CatalogSourceReconciler) Get(ctx context.Context, id deppy.Identifier) *input.Entity {
	r.RWMutex.RLock()
	defer r.RWMutex.RUnlock()
	// don't count on deppy ID to reflect its catalogsource
	for _, entries := range r.cache {
		for _, entity := range entries.Items {
			if entity.Identifier() == id {
				return entity
			}
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
	for _, entries := range r.cache {
		for _, entity := range entries.Items {
			if err := fn(entity); err != nil {
				return err
			}
		}
	}
	return nil
}
