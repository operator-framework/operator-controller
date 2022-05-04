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
	"fmt"
	"time"

	"github.com/blang/semver/v4"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-registry/pkg/api"
	registryClient "github.com/operator-framework/operator-registry/pkg/client"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

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

type candidateBundles []*api.Bundle

//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators/finalizers,verbs=update
//+kubebuilder:rbac:groups=operators.coreos.com,resources=catalogsources,verbs=get;list;watch
//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundleinstances,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundles,verbs=get;list;watch;create;update;patch;delete

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
		log.Error(err, "failed to find the platform operator")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("listing the platform-operators catalogsource")
	cs := &operatorsv1alpha1.CatalogSource{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      catalogSourceName,
		Namespace: catalogSourceNamespace,
	}, cs); err != nil {
		log.Error(err, "failed to find the platform operators catalogsource")
		return ctrl.Result{}, err
	}

	log.Info("listing bundles from context")
	it, err := r.RegistryClient.ListBundles(ctx)
	if err != nil {
		log.Error(err, "failed to list bundles in the platform operators catalog source")
		return ctrl.Result{}, err
	}

	log.Info("filtering out bundles into candidates", "package name", po.Spec.PackageName, "channel name", "4.12")
	var cb candidateBundles
	for b := it.Next(); b != nil; b = it.Next() {
		log.Info("processes bundle", "name", b.GetPackageName())
		if b.PackageName != po.Spec.PackageName {
			continue
		}
		if b.ChannelName != "4.12" {
			continue
		}
		cb = append(cb, b)
	}
	if len(cb) == 0 {
		log.Info("failed to find any candidate bundles")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// TODO: move to a method/function
	// TODO: figure out what's the most appropriate field to parse by semver range. CsvName maybe if that's constantly present?
	log.Info("finding the bundles that contain the highest semver")

	latestBundle, err := cb.latest()
	if err != nil || latestBundle == nil {
		log.Info("failed to find the bundle with the highest semver")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, err
	}

	log.Info("candidate bundles", "highest semver", latestBundle.Version)

	// TODO: figure out what's the most appropriate field to parse by semver range. CsvName maybe if that's constantly present?
	// TODO: check whether we need to pivot the bundle
	// TODO: what happens when the bundle failed?
	// TODO: watch the bundle resource?
	if err := r.ensureBundle(ctx, po, latestBundle.BundlePath, latestBundle.CsvName); err != nil {
		log.Error(err, "failed to generate the bundle resource")
		return ctrl.Result{}, err
	}
	if err := r.Status().Update(ctx, po); err != nil {
		log.Error(err, "failed to update the platform operator status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PlatformOperatorReconciler) ensureBundle(ctx context.Context, po *platformopenshiftiov1alpha1.PlatformOperator, bundleName, bundleImage string) error {
	if po.Status.InstalledBundleName != "" {
		return nil
	}
	bundle := &rukpakv1alpha1.Bundle{}
	if err := r.Get(ctx, types.NamespacedName{Name: bundleName}, bundle); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		bundle = &rukpakv1alpha1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name: bundleName,
			},
			Spec: rukpakv1alpha1.BundleSpec{
				ProvisionerClassName: "core.rukpak.io/plain",
				Source: rukpakv1alpha1.BundleSource{
					Type: rukpakv1alpha1.SourceTypeImage,
					Image: &rukpakv1alpha1.ImageSource{
						Ref: bundleImage,
					},
				},
			},
		}
		if err := controllerutil.SetOwnerReference(po, bundle, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, bundle); err != nil {
			return err
		}
	}
	po.Status.InstalledBundleName = bundleName

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlatformOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformopenshiftiov1alpha1.PlatformOperator{}).
		Watches(&source.Kind{Type: &operatorsv1alpha1.CatalogSource{}}, handler.EnqueueRequestsFromMapFunc(requeuePlatformOperators(mgr.GetClient()))).
		Complete(r)
}

func requeuePlatformOperators(cl client.Client) handler.MapFunc {
	return func(object client.Object) []reconcile.Request {
		poList := &platformopenshiftiov1alpha1.PlatformOperatorList{}
		if err := cl.List(context.Background(), poList); err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, po := range poList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: po.GetName(),
				},
			})
		}
		return requests
	}
}

func (cb candidateBundles) latest() (*api.Bundle, error) {
	var (
		highestSemver semver.Version
		latestBundle  *api.Bundle
	)
	for _, bundle := range cb {
		currVer, err := semver.Parse(bundle.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to parse the bundle %s semver: %v", bundle.CsvJson, err)
		}
		if currVer.Compare(highestSemver) == 1 {
			highestSemver = currVer
			latestBundle = bundle
		}
	}

	return latestBundle, nil
}
