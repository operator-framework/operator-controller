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

	platformopenshiftiov1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
	"github.com/timflannagan/platform-operators/internal/updater"
)

const (
	channelName            = "4.12"
	catalogReconnectTime   = time.Second * 5
	catalogSourceName      = "platform-operators-catalog-source"
	catalogSourceNamespace = "platform-operators-system"
)

var catalogSourceAddress = catalogSourceName + "." + catalogSourceNamespace + ".svc:50051"

// PlatformOperatorReconciler reconciles a PlatformOperator object
type PlatformOperatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlatformOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformopenshiftiov1alpha1.PlatformOperator{}).
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
			return nil, fmt.Errorf("failed to parse the bundle %s semver: %v", bundle.CsvJson, err)
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

	po := &platformopenshiftiov1alpha1.PlatformOperator{}
	if err := r.Get(ctx, req.NamespacedName, po); err != nil {
		log.Error(err, "failed to find the platform operator")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	u := updater.NewPlatformOperatorUpdater(r.Client)
	defer func() {
		if err := u.Apply(ctx, po); err != nil {
			log.Error(err, "failed to update status")
		}
	}()

	u.UpdateStatus(
		updater.SetPhase(platformopenshiftiov1alpha1.PhaseFailing),
		updater.EnsureCondition(metav1.Condition{
			Type:    platformopenshiftiov1alpha1.TypeInstalled,
			Status:  metav1.ConditionUnknown,
			Reason:  platformopenshiftiov1alpha1.ReasonInstalling,
			Message: "",
		}),
	)

	rc, err := handleCatalogConnection(catalogSourceAddress)
	if err != nil {
		log.Error(err, "failed to connect to the requisite catalog")
		u.UpdateStatus(
			updater.SetPhase(platformopenshiftiov1alpha1.PhaseFailing),
			updater.EnsureCondition(metav1.Condition{
				Type:    platformopenshiftiov1alpha1.TypeInstalled,
				Status:  metav1.ConditionFalse,
				Reason:  platformopenshiftiov1alpha1.ReasonCatalogUnreachable,
				Message: err.Error(),
			}),
		)
		return ctrl.Result{}, err
	}

	log.Info("listing bundles from context")
	it, err := rc.ListBundles(ctx)
	if err != nil {
		log.Error(err, "failed to list bundles in the platform operators catalog source")
		u.UpdateStatus(
			updater.SetPhase(platformopenshiftiov1alpha1.PhaseFailing),
			updater.EnsureCondition(metav1.Condition{
				Type:    platformopenshiftiov1alpha1.TypeInstalled,
				Status:  metav1.ConditionFalse,
				Reason:  platformopenshiftiov1alpha1.ReasonCatalogUnstable,
				Message: err.Error(),
			}),
		)
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

	latestBundle, err := cb.latest()
	if err != nil || latestBundle == nil {
		message := "failed to find a valid bundle"
		log.Info(message)
		u.UpdateStatus(
			updater.SetPhase(platformopenshiftiov1alpha1.PhaseFailing),
			updater.EnsureCondition(metav1.Condition{
				Type:    platformopenshiftiov1alpha1.TypeInstalled,
				Status:  metav1.ConditionFalse,
				Reason:  platformopenshiftiov1alpha1.ReasonNoValidBundles,
				Message: fmt.Sprintf("%s with error: %v", message, err),
			}),
		)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, err
	}

	log.Info("found a valid bundle", "bundle", latestBundle.CsvName, "version", latestBundle.Version)

	// TODO: what happens when the bundle failed?
	if err := r.ensureBundleInstance(ctx, po, latestBundle); err != nil {
		log.Error(err, "failed to generate the bundle resource")
		u.UpdateStatus(
			updater.SetPhase(platformopenshiftiov1alpha1.PhaseFailing),
			updater.EnsureCondition(metav1.Condition{
				Type:    platformopenshiftiov1alpha1.TypeInstalled,
				Status:  metav1.ConditionFalse,
				Reason:  platformopenshiftiov1alpha1.ReasonBundleInstanceFailed,
				Message: err.Error(),
			}),
		)
		return ctrl.Result{}, err
	}

	u.UpdateStatus(
		updater.SetPhase(platformopenshiftiov1alpha1.PhaseFailing),
		updater.EnsureCondition(metav1.Condition{
			Type:    platformopenshiftiov1alpha1.TypeInstalled,
			Status:  metav1.ConditionTrue,
			Reason:  platformopenshiftiov1alpha1.ReasonInstallSuccessful,
			Message: "",
		}),
	)

	return ctrl.Result{}, nil
}

func (r *PlatformOperatorReconciler) ensureBundleInstance(ctx context.Context, po *platformopenshiftiov1alpha1.PlatformOperator, bundle *api.Bundle) error {
	bi := &rukpakv1alpha1.BundleInstance{}
	bi.SetName(po.GetName())
	controllerRef := metav1.NewControllerRef(po, po.GroupVersionKind())

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, bi, func() error {
		bi.SetOwnerReferences([]metav1.OwnerReference{*controllerRef})
		bi.Spec = *buildBundleInstance(bi.GetName(), bundle.BundlePath)

		return nil
	})

	return err
}

// handleCatalogConnection takes a catalogSourceService and returns a registryClient.Client
// if the catalog is reachable
func handleCatalogConnection(address string) (*registryClient.Client, error) {
	rc, err := registryClient.NewClient(address)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client from %s catalog source: %v", address, err)
	}
	if healthy, err := rc.HealthCheck(context.Background(), catalogReconnectTime); !healthy || err != nil {
		return nil, fmt.Errorf("failed to connect to %s catalog source via the registry client: %v", address, err)
	}

	return rc, nil
}

// createBundleInstance is responsible for taking a name and image to create an embedded BundleInstance
func buildBundleInstance(name, image string) *rukpakv1alpha1.BundleInstanceSpec {
	return &rukpakv1alpha1.BundleInstanceSpec{
		ProvisionerClassName: "core.rukpak.io/plain",
		Template: &rukpakv1alpha1.BundleTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": name},
			},
			Spec: rukpakv1alpha1.BundleSpec{
				ProvisionerClassName: "core.rukpak.io/plain",
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

func requeueBundleInstance(c client.Client) handler.MapFunc {
	return func(obj client.Object) []reconcile.Request {
		bi := obj.(*rukpakv1alpha1.BundleInstance)

		poList := &platformopenshiftiov1alpha1.PlatformOperatorList{}
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
