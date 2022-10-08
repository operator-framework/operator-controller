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

package core

import (
	"context"

	opm "github.com/operator-framework/operator-registry/alpha/action"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	bundleProperty "github.com/operator-framework/operator-registry/alpha/property"
	"github.com/operator-framework/operator-registry/pkg/image/containerdregistry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1beta1 "github.com/anik120/rukpak-packageserver/pkg/apis/core/v1beta1"
)

// CatalogSourceReconciler reconciles a CatalogSource object
type CatalogSourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=core.rukpak.io,resources=catalogsources,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.rukpak.io,resources=catalogsources/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.rukpak.io,resources=catalogsources/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CatalogSource object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *CatalogSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	catalogSource := corev1beta1.CatalogSource{}
	if err := r.Client.Get(ctx, req.NamespacedName, &catalogSource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// IMPORTANT TODO: This implementation of containerdregistry requires privileged perm to create a CacheDir
	// Figure out a way to not use the CacheDir so that container can be run in non-privileged mode.
	reg, err := containerdregistry.NewRegistry()
	// defer reg.Destroy()
	if err != nil {
		return ctrl.Result{}, err
	}
	imageRenderer := opm.Render{Refs: []string{catalogSource.Spec.Image}, Registry: reg}
	declCfg, err := imageRenderer.Run(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.buildCache(ctx, declCfg, catalogSource, req); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.buildPackages(ctx, declCfg, catalogSource, req); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CatalogSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1beta1.CatalogSource{}).
		Complete(r)
}

func (r *CatalogSourceReconciler) buildCache(ctx context.Context, declCfg *declcfg.DeclarativeConfig, catalogSource corev1beta1.CatalogSource, req ctrl.Request) error {
	cache := corev1beta1.CatalogCache{ObjectMeta: metav1.ObjectMeta{
		Name:      req.Name,
		Namespace: req.Namespace,
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion: catalogSource.APIVersion,
				Kind:       catalogSource.Kind,
				Name:       req.Name,
				UID:        catalogSource.UID,
			},
		}}}
	for _, bundle := range declCfg.Bundles {
		operator := corev1beta1.Operator{
			Name:       bundle.Name,
			Package:    bundle.Package,
			BundlePath: bundle.Image,
		}
		props, _ := bundleProperty.Parse(bundle.Properties)
		providedGVKs := []corev1beta1.APIKey{}
		for _, gvk := range props.GVKs {
			providedGVKs = append(providedGVKs, corev1beta1.APIKey{Group: gvk.Group, Kind: gvk.Kind, Version: gvk.Version})
		}
		requiredGVKs := []corev1beta1.APIKey{}
		for _, gvk := range props.GVKsRequired {
			requiredGVKs = append(requiredGVKs, corev1beta1.APIKey{Group: gvk.Group, Kind: gvk.Kind, Version: gvk.Version})
		}
		operator.ProvidedAPIs = providedGVKs
		operator.RequiredAPIs = requiredGVKs
		cache.Spec.Operators = append(cache.Spec.Operators, operator)
	}

	if err := r.Client.Create(ctx, &cache); err != nil {
		return err
	}
	return nil
}

func (r *CatalogSourceReconciler) buildPackages(ctx context.Context, declCfg *declcfg.DeclarativeConfig, catalogSource corev1beta1.CatalogSource, req ctrl.Request) error {

	for _, pkg := range declCfg.Packages {

		pack := corev1beta1.Package{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pkg.Name,
				Namespace: req.Namespace,
			},
			Spec: corev1beta1.PackageSpec{
				CatalogSource:          catalogSource.Name,
				CatalogSourceNamespace: catalogSource.Namespace,
				DefaultChannel:         pkg.DefaultChannel,
				Icon: corev1beta1.Icon{
					Base64Data: string(pkg.Icon.Data),
					Mediatype:  pkg.Icon.MediaType,
				},
			},
		}
		for _, ch := range declCfg.Channels {
			if ch.Package == pkg.Name {
				pack.Spec.Channels = append(pack.Spec.Channels, corev1beta1.PackageChannel{
					Name: ch.Name,
					// Head: ,
				})
			}
		}

		if err := r.Client.Create(ctx, &pack); err != nil {
			return err
		}
	}
	return nil
}
