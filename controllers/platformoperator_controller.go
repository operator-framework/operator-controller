/*
Copyright 2022.

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

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	registryClient "github.com/operator-framework/operator-registry/pkg/client"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logr "sigs.k8s.io/controller-runtime/pkg/log"

	platformopenshiftiov1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
)

const (
	catalogSourceName      = "platform-operators-catalog-source"
	catalogSourceNamespace = "platform-operators-system"
)

// PlatformOperatorReconciler reconciles a PlatformOperator object
type PlatformOperatorReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	RegistryClient registryClient.Interface
}

//+kubebuilder:rbac:groups=platform.openshift.io.my.domain,resources=platformoperators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=platform.openshift.io.my.domain,resources=platformoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=platform.openshift.io.my.domain,resources=platformoperators/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *PlatformOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logr.FromContext(ctx)
	log.Info("reconciling request", "req", req.NamespacedName)
	defer log.Info("finished reconciling request", "req", req.NamespacedName)

	po := &platformopenshiftiov1alpha1.PlatformOperator{}
	if err := r.Get(ctx, req.NamespacedName, po); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	cs := &operatorsv1alpha1.CatalogSource{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      catalogSourceName,
		Namespace: "platform-operator-system",
	}, cs); err != nil {
		log.Error(err, "failed to find the platform operators catalogsource")
		return ctrl.Result{}, err
	}

	it, err := r.RegistryClient.ListBundles(ctx)
	if err != nil {
		log.Error(err, "failed to list bundles in the platform operators catalog source")
		return ctrl.Result{}, err
	}
	for b := it.Next(); b != nil; b = it.Next() {
		log.Info("bundle iterator", "bundle", b.String())
	}

	/*
			TODO:
			- Need a status installed vs. desired package version?
		    - Spec field for which CatalogSource to use?

			Steps:
			- Connect to the CatalogSource
			- Get the highest semver range
			- Check whether we have already generated a BundleInstance that matches that metadata.Name
	*/

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlatformOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformopenshiftiov1alpha1.PlatformOperator{}).
		Complete(r)
}

// func (s *registrySource) Snapshot(ctx context.Context) (*cache.Snapshot, error) {
// 	// Fetching default channels this way makes many round trips
// 	// -- may need to either add a new API to fetch all at once,
// 	// or embed the information into Bundle.
// 	defaultChannels := make(map[string]string)

// 	it, err := s.client.ListBundles(ctx)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to list bundles: %w", err)
// 	}

// 	var operators []*cache.Entry
// 	for b := it.Next(); b != nil; b = it.Next() {
// 		defaultChannel, ok := defaultChannels[b.PackageName]
// 		if !ok {
// 			if p, err := s.client.GetPackage(ctx, b.PackageName); err != nil {
// 				s.logger.Printf("failed to retrieve default channel for bundle, continuing: %v", err)
// 				continue
// 			} else {
// 				defaultChannels[b.PackageName] = p.DefaultChannelName
// 				defaultChannel = p.DefaultChannelName
// 			}
// 		}
// 		o, err := newOperatorFromBundle(b, "", s.key, defaultChannel)
// 		if err != nil {
// 			s.logger.Printf("failed to construct operator from bundle, continuing: %v", err)
// 			continue
// 		}
// 		o.ProvidedAPIs = o.ProvidedAPIs.StripPlural()
// 		o.RequiredAPIs = o.RequiredAPIs.StripPlural()
// 		o.Replaces = b.Replaces
// 		EnsurePackageProperty(o, b.PackageName, b.Version)
// 		operators = append(operators, o)
// 	}
// 	if err := it.Error(); err != nil {
// 		return nil, fmt.Errorf("error encountered while listing bundles: %w", err)
// 	}

// 	return &cache.Snapshot{
// 		Entries: operators,
// 		Valid:   s.invalidator.GetValidChannel(s.key),
// 	}, nil
// }
