/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"io/fs"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type CatalogCache interface {
	Get(catalogName, resolvedRef string) (fs.FS, error)
	Remove(catalogName string) error
}

type CatalogCachePopulator interface {
	PopulateCache(ctx context.Context, catalog *ocv1.ClusterCatalog) (fs.FS, error)
}

// ClusterCatalogReconciler reconciles a ClusterCatalog object
type ClusterCatalogReconciler struct {
	client.Client
	CatalogCache          CatalogCache
	CatalogCachePopulator CatalogCachePopulator
}

func (r *ClusterCatalogReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("cluster-catalog")
	ctx = log.IntoContext(ctx, l)

	l.Info("reconcile starting")
	defer l.Info("reconcile ending")

	existingCatalog := &ocv1.ClusterCatalog{}
	err := r.Get(ctx, req.NamespacedName, existingCatalog)
	if apierrors.IsNotFound(err) {
		if err := r.CatalogCache.Remove(req.Name); err != nil {
			return ctrl.Result{}, fmt.Errorf("error removing cache for catalog %q: %v", req.Name, err)
		}
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if existingCatalog.Status.ResolvedSource == nil ||
		existingCatalog.Status.ResolvedSource.Image == nil ||
		existingCatalog.Status.ResolvedSource.Image.Ref == "" {
		// Reference is not known yet - skip cache population with no error.
		// Once the reference is resolved another reconcile cycle
		// will be triggered and we will progress further.
		return ctrl.Result{}, nil
	}

	catalogFsys, err := r.CatalogCache.Get(existingCatalog.Name, existingCatalog.Status.ResolvedSource.Image.Ref)
	if err != nil {
		l.Info("retrying cache population: found previous error from catalog cache", "cacheErr", err)
	} else if catalogFsys != nil {
		// Cache already exists so we do not need to populate it
		return ctrl.Result{}, nil
	}

	if _, err = r.CatalogCachePopulator.PopulateCache(ctx, existingCatalog); err != nil {
		return ctrl.Result{}, fmt.Errorf("error populating cache for catalog %q: %v", existingCatalog.Name, err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterCatalogReconciler) SetupWithManager(mgr ctrl.Manager) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		Named("controller-operator-clustercatalog-controller").
		For(&ocv1.ClusterCatalog{}).
		Build(r)

	return err
}
