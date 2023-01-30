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
	"fmt"

	opm "github.com/operator-framework/operator-registry/alpha/action"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
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

	if err := r.buildPackages(ctx, declCfg, catalogSource); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.buildBundleMetadata(ctx, declCfg, catalogSource); err != nil {
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

func (r *CatalogSourceReconciler) buildBundleMetadata(ctx context.Context, declCfg *declcfg.DeclarativeConfig, catalogSource corev1beta1.CatalogSource) error {
	for _, bundle := range declCfg.Bundles {
		bundleMeta := corev1beta1.BundleMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name: bundle.Name,
			},
			Spec: corev1beta1.BundleMetadataSpec{
				CatalogSource: catalogSource.Name,
				Package:       bundle.Package,
				Image:         bundle.Image,
				Properties:    []corev1beta1.Property{},
				RelatedImages: []corev1beta1.RelatedImage{},
			},
		}

		for _, relatedImage := range bundle.RelatedImages {
			bundleMeta.Spec.RelatedImages = append(bundleMeta.Spec.RelatedImages, corev1beta1.RelatedImage{
				Name:  relatedImage.Name,
				Image: relatedImage.Image,
			})
		}

		for _, prop := range bundle.Properties {
			// skip any properties that are of type `olm.bundle.object`
			if prop.Type == "olm.bundle.object" {
				continue
			}

			bundleMeta.Spec.Properties = append(bundleMeta.Spec.Properties, corev1beta1.Property{
				Type:  prop.Type,
				Value: prop.Value,
			})
		}

		if err := r.Client.Create(ctx, &bundleMeta); err != nil {
			return fmt.Errorf("creating bundlemetadata %q: %w", bundleMeta.Name, err)
		}
	}

	return nil
}

func (r *CatalogSourceReconciler) buildPackages(ctx context.Context, declCfg *declcfg.DeclarativeConfig, catalogSource corev1beta1.CatalogSource) error {
	for _, pkg := range declCfg.Packages {
		pack := corev1beta1.Package{
			ObjectMeta: metav1.ObjectMeta{
				// TODO: If we just provide the name of the package, then
				// we are inherently saying no other catalog sources can provide a package
				// of the same name due to this being a cluster scoped resource. We should
				// look into options for configuring admission criteria for the Package
				// resource to resolve this potential clash.
				Name: pkg.Name,
			},
			Spec: corev1beta1.PackageSpec{
				CatalogSource:  catalogSource.Name,
				DefaultChannel: pkg.DefaultChannel,
				Channels:       []corev1beta1.PackageChannel{},
				Description:    pkg.Description,
			},
		}
		for _, ch := range declCfg.Channels {
			if ch.Package == pkg.Name {
				packChannel := corev1beta1.PackageChannel{
					Name:    ch.Name,
					Entries: []corev1beta1.ChannelEntry{},
				}
				for _, entry := range ch.Entries {
					packChannel.Entries = append(packChannel.Entries, corev1beta1.ChannelEntry{
						Name:      entry.Name,
						Replaces:  entry.Replaces,
						Skips:     entry.Skips,
						SkipRange: entry.SkipRange,
					})
				}

				pack.Spec.Channels = append(pack.Spec.Channels, packChannel)
			}
		}

		if err := r.Client.Create(ctx, &pack); err != nil {
			return fmt.Errorf("creating package %q: %w", pack.Name, err)
		}
	}
	return nil
}
