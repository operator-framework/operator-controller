package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	catalogd "github.com/operator-framework/catalogd/pkg/apis/core/v1beta1"
	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Predicate for reconciling operators when available (i.e. ready) catalogsources on cluster change
type catalogReadyTransitionPredicate struct {
	predicate.Funcs
	catalogReady map[string]bool
}

func newCatalogReadyTransitionPredicate() *catalogReadyTransitionPredicate {
	return &catalogReadyTransitionPredicate{
		catalogReady: map[string]bool{},
	}
}

func (c *catalogReadyTransitionPredicate) Create(e event.CreateEvent) bool {
	fmt.Println("CreateEvent CatalogSource", e.Object.GetName())
	catalogReady, err := isCatalogReady(e.Object)
	if err != nil {
		fmt.Println(err)
		return false
	}
	c.catalogReady[e.Object.GetName()] = catalogReady
	return catalogReady
}

func (c *catalogReadyTransitionPredicate) Update(e event.UpdateEvent) bool {
	fmt.Println("UpdateEvent CatalogSource", e.ObjectOld.GetName(), e.ObjectNew.GetName())
	oldCatalogReady, err := isCatalogReady(e.ObjectOld)
	if err != nil {
		fmt.Println(err)
		return false
	}

	newCatalogReady, err := isCatalogReady(e.ObjectNew)
	if err != nil {
		fmt.Println(err)
		return false
	}

	c.catalogReady[e.ObjectNew.GetName()] = newCatalogReady
	// TODO: determine if ready -> non-ready transition triggers reconcile with stale catalog contents
	return oldCatalogReady != newCatalogReady
}

func (c *catalogReadyTransitionPredicate) Delete(e event.DeleteEvent) bool {
	fmt.Println("DeleteEvent CatalogSource", e.Object.GetName())
	delete(c.catalogReady, e.Object.GetName())
	return true
}

func (c *catalogReadyTransitionPredicate) Generic(e event.GenericEvent) bool {
	fmt.Println("GenericEvent CatalogSource", e.Object.GetName())
	catalogReady, err := isCatalogReady(e.Object)
	if err != nil {
		fmt.Println(err)
		return false
	}
	predicateState := c.catalogReady[e.Object.GetName()] != catalogReady
	c.catalogReady[e.Object.GetName()] = catalogReady
	return predicateState
}

func isCatalogReady(o client.Object) (bool, error) {
	catalog, ok := o.(*catalogd.CatalogSource)
	if !ok {
		return false, fmt.Errorf("wrong object type: not a catalogsource: %+v", o)
	}
	if len(catalog.Status.Conditions) > 0 {
		for _, cond := range catalog.Status.Conditions {
			if cond.Type == catalogd.TypeReady && cond.Status == v1.ConditionTrue {
				return true, nil
			}
		}
	}
	return false, nil
}

// Generate reconcile requests for all operators affected by a catalog change
func operatorRequestsForCatalog(ctx context.Context, c client.Client, logger logr.Logger) handler.MapFunc {
	return func(object client.Object) []reconcile.Request {
		// no way of associating an operator to a catalog so create reconcile requests for everything
		operators := operatorv1alpha1.OperatorList{}
		err := c.List(ctx, &operators)
		if err != nil {
			logger.Error(err, "unable to enqueue operators for catalog reconcile")
			return nil
		}
		var requests []reconcile.Request
		for _, op := range operators.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: op.GetNamespace(),
					Name:      op.GetName(),
				},
			})
		}
		return requests
	}
}
