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

	platformv1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
)

const (
	channelName           = "4.12"
	plainProvisionerID    = "core.rukpak.io/plain"
	registryProvisionerID = "core.rukpak.io/registry"
)

// PlatformOperatorReconciler reconciles a PlatformOperator object
type PlatformOperatorReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	RegistryClient registryClient.Interface
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlatformOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.PlatformOperator{}).
		Watches(&source.Kind{Type: &operatorsv1alpha1.CatalogSource{}}, handler.EnqueueRequestsFromMapFunc(requeuePlatformOperators(mgr.GetClient()))).
		Watches(&source.Kind{Type: &rukpakv1alpha1.BundleInstance{}}, handler.EnqueueRequestsFromMapFunc(requeueBundleInstance(mgr.GetClient()))).
		Complete(r)
}

type candidateBundles []*api.Bundle

func (cb candidateBundles) latest() (*api.Bundle, error) {
	var (
		highestSemver semver.Version
		latestBundle  *api.Bundle
	)
	for _, bundle := range cb {
		currVer, err := semver.Parse(bundle.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to parse the bundle %s semver: %w", bundle.CsvJson, err)
		}
		if currVer.Compare(highestSemver) == 1 {
			highestSemver = currVer
			latestBundle = bundle
		}
	}

	return latestBundle, nil
}

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

	po := &platformv1alpha1.PlatformOperator{}
	if err := r.Get(ctx, req.NamespacedName, po); err != nil {
		log.Error(err, "failed to find the platform operator")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	css := &operatorsv1alpha1.CatalogSourceList{}
	if err := r.List(ctx, css); err != nil {
		log.Error(err, "failed to list the catalogsource resources in the cluster")
		return ctrl.Result{}, err
	}
	if len(css.Items) == 0 {
		log.Info("unable to query catalog content as no catalogsources are available")
		return ctrl.Result{}, nil
	}
	// TODO(tflannag): properly handle multiple catalogsources in a cluster
	cs := css.Items[0]

	log.Info("creating registry client from catalogsource")
	rc, err := registryClient.NewClient(cs.Spec.Address)
	if err != nil {
		log.Error(err, "failed to create registry client from catalogsource", "name", cs.GetName(), "namespace", cs.GetNamespace(), "address", cs.Spec.Address)
		return ctrl.Result{}, err
	}

	log.Info("listing bundles from context")
	it, err := rc.ListBundles(ctx)
	if err != nil {
		log.Error(err, "failed to list bundles in the platform operators catalog source")
		return ctrl.Result{}, err
	}

	log.Info("filtering out bundles into candidates", "package name", po.Spec.PackageName, "channel name", channelName)
	var cb candidateBundles
	// TODO: Should build a cache for efficiency
	for b := it.Next(); b != nil; b = it.Next() {
		if b.PackageName != po.Spec.PackageName || b.ChannelName != channelName {
			continue
		}
		cb = append(cb, b)
	}
	if len(cb) == 0 {
		log.Info("failed to find any candidate bundles")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	latestBundle, err := cb.latest()
	if err != nil || latestBundle == nil {
		log.Info("failed to find the bundle with the highest semver")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, err
	}

	log.Info("candidate bundle", "bundle", latestBundle.CsvName, "version", latestBundle.Version)

	// TODO: figure out what's the most appropriate field to parse by semver range. CsvName maybe if that's constantly present?
	// TODO: what happens when the bundle failed?
	if err := r.ensureBundleInstance(ctx, po, latestBundle); err != nil {
		log.Error(err, "failed to generate the bundle resource")
		return ctrl.Result{}, err
	}

	if err := r.Status().Update(ctx, po); err != nil {
		log.Error(err, "failed to update the platform operator status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PlatformOperatorReconciler) ensureBundleInstance(ctx context.Context, po *platformv1alpha1.PlatformOperator, bundle *api.Bundle) error {
	bi := &rukpakv1alpha1.BundleInstance{}
	bi.SetName(po.GetName())
	controllerRef := metav1.NewControllerRef(po, po.GroupVersionKind())

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, bi, func() error {
		bi.SetOwnerReferences([]metav1.OwnerReference{*controllerRef})
		bi.Spec = *buildBundleInstance(bundle.BundlePath)
		return nil
	})
	return err
}

// createBundleInstance is responsible for taking a name and image to create an embedded BundleInstance
func buildBundleInstance(image string) *rukpakv1alpha1.BundleInstanceSpec {
	return &rukpakv1alpha1.BundleInstanceSpec{
		ProvisionerClassName: plainProvisionerID,
		Template: &rukpakv1alpha1.BundleTemplate{
			Spec: rukpakv1alpha1.BundleSpec{
				ProvisionerClassName: registryProvisionerID,
				Source: rukpakv1alpha1.BundleSource{
					Type: rukpakv1alpha1.SourceTypeImage,
					Image: &rukpakv1alpha1.ImageSource{
						Ref: image,
					},
				},
			},
		},
	}
}

func requeuePlatformOperators(cl client.Client) handler.MapFunc {
	return func(object client.Object) []reconcile.Request {
		poList := &platformv1alpha1.PlatformOperatorList{}
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

func requeueBundleInstance(c client.Client) handler.MapFunc {
	return func(obj client.Object) []reconcile.Request {
		bi := obj.(*rukpakv1alpha1.BundleInstance)

		poList := &platformv1alpha1.PlatformOperatorList{}
		if err := c.List(context.Background(), poList); err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, po := range poList.Items {
			po := po

			for _, ref := range bi.GetOwnerReferences() {
				if ref.Name == po.GetName() {
					requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&po)})
				}
			}
		}
		return requests
	}
}
