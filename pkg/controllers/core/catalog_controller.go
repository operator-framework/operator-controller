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
	// #nosec
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimacherrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/catalogd/internal/source"
	"github.com/operator-framework/catalogd/pkg/features"
)

// TODO (everettraven): Add unit tests for the CatalogReconciler

// CatalogReconciler reconciles a Catalog object
type CatalogReconciler struct {
	client.Client
	Unpacker source.Unpacker
}

//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogs/finalizers,verbs=update
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=bundlemetadata,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=bundlemetadata/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=bundlemetadata/finalizers,verbs=update
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=packages,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=packages/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=packages/finalizers,verbs=update
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogmetadata,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=create;update;patch;delete;get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods/log,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *CatalogReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// TODO: Where and when should we be logging errors and at which level?
	_ = log.FromContext(ctx).WithName("catalogd-controller")

	existingCatsrc := v1alpha1.Catalog{}
	if err := r.Client.Get(ctx, req.NamespacedName, &existingCatsrc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reconciledCatsrc := existingCatsrc.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledCatsrc)

	// Update the status subresource before updating the main object. This is
	// necessary because, in many cases, the main object update will remove the
	// finalizer, which will cause the core Kubernetes deletion logic to
	// complete. Therefore, we need to make the status update prior to the main
	// object update to ensure that the status update can be processed before
	// a potential deletion.
	if !equality.Semantic.DeepEqual(existingCatsrc.Status, reconciledCatsrc.Status) {
		if updateErr := r.Client.Status().Update(ctx, reconciledCatsrc); updateErr != nil {
			return res, apimacherrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}
	existingCatsrc.Status, reconciledCatsrc.Status = v1alpha1.CatalogStatus{}, v1alpha1.CatalogStatus{}
	if !equality.Semantic.DeepEqual(existingCatsrc, reconciledCatsrc) {
		if updateErr := r.Client.Update(ctx, reconciledCatsrc); updateErr != nil {
			return res, apimacherrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}
	return res, reconcileErr
}

// SetupWithManager sets up the controller with the Manager.
func (r *CatalogReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// TODO: Due to us not having proper error handling,
		// not having this results in the controller getting into
		// an error state because once we update the status it requeues
		// and then errors out when trying to create all the Packages again
		// even though they already exist. This should be resolved by the fix
		// for https://github.com/operator-framework/catalogd/issues/6. The fix for
		// #6 should also remove the usage of `builder.WithPredicates(predicate.GenerationChangedPredicate{})`
		For(&v1alpha1.Catalog{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.Pod{}).
		Complete(r)
}

// Note: This function always returns ctrl.Result{}. The linter
// fusses about this as we could instead just return error. This was
// discussed in https://github.com/operator-framework/rukpak/pull/635#discussion_r1229859464
// and the consensus was that it is better to keep the ctrl.Result return
// type so that if we do end up needing to return something else we don't forget
// to add the ctrl.Result type back as a return value. Adding a comment to ignore
// linting from the linter that was fussing about this.
// nolint:unparam
func (r *CatalogReconciler) reconcile(ctx context.Context, catalog *v1alpha1.Catalog) (ctrl.Result, error) {
	unpackResult, err := r.Unpacker.Unpack(ctx, catalog)
	if err != nil {
		return ctrl.Result{}, updateStatusUnpackFailing(&catalog.Status, fmt.Errorf("source bundle content: %v", err))
	}

	switch unpackResult.State {
	case source.StatePending:
		updateStatusUnpackPending(&catalog.Status, unpackResult)
		return ctrl.Result{}, nil
	case source.StateUnpacking:
		updateStatusUnpacking(&catalog.Status, unpackResult)
		return ctrl.Result{}, nil
	case source.StateUnpacked:
		// TODO: We should check to see if the unpacked result has the same content
		//   as the already unpacked content. If it does, we should skip this rest
		//   of the unpacking steps.

		fbc, err := declcfg.LoadFS(unpackResult.FS)
		if err != nil {
			return ctrl.Result{}, updateStatusUnpackFailing(&catalog.Status, fmt.Errorf("load FBC from filesystem: %v", err))
		}

		if features.CatalogdFeatureGate.Enabled(features.PackagesBundleMetadataAPIs) {
			if err := r.syncPackages(ctx, fbc, catalog); err != nil {
				return ctrl.Result{}, updateStatusUnpackFailing(&catalog.Status, fmt.Errorf("create package objects: %v", err))
			}

			if err := r.syncBundleMetadata(ctx, fbc, catalog); err != nil {
				return ctrl.Result{}, updateStatusUnpackFailing(&catalog.Status, fmt.Errorf("create bundle metadata objects: %v", err))
			}
		}

		if features.CatalogdFeatureGate.Enabled(features.CatalogMetadataAPI) {
			if err = r.syncCatalogMetadata(ctx, unpackResult.FS, catalog); err != nil {
				return ctrl.Result{}, updateStatusUnpackFailing(&catalog.Status, fmt.Errorf("create catalog metadata objects: %v", err))
			}
		}

		updateStatusUnpacked(&catalog.Status, unpackResult)
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, updateStatusUnpackFailing(&catalog.Status, fmt.Errorf("unknown unpack state %q: %v", unpackResult.State, err))
	}
}

func updateStatusUnpackPending(status *v1alpha1.CatalogStatus, result *source.Result) {
	status.ResolvedSource = nil
	status.Phase = v1alpha1.PhasePending
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    v1alpha1.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ReasonUnpackPending,
		Message: result.Message,
	})
}

func updateStatusUnpacking(status *v1alpha1.CatalogStatus, result *source.Result) {
	status.ResolvedSource = nil
	status.Phase = v1alpha1.PhaseUnpacking
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    v1alpha1.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ReasonUnpacking,
		Message: result.Message,
	})
}

func updateStatusUnpacked(status *v1alpha1.CatalogStatus, result *source.Result) {
	status.ResolvedSource = result.ResolvedSource
	status.Phase = v1alpha1.PhaseUnpacked
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    v1alpha1.TypeUnpacked,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.ReasonUnpackSuccessful,
		Message: result.Message,
	})
}

func updateStatusUnpackFailing(status *v1alpha1.CatalogStatus, err error) error {
	status.ResolvedSource = nil
	status.Phase = v1alpha1.PhaseFailing
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    v1alpha1.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ReasonUnpackFailed,
		Message: err.Error(),
	})
	return err
}

// syncBundleMetadata will create a `BundleMetadata` resource for each
// "olm.bundle" object that exists for the given catalog contents. Returns an
// error if any are encountered.
func (r *CatalogReconciler) syncBundleMetadata(ctx context.Context, declCfg *declcfg.DeclarativeConfig, catalog *v1alpha1.Catalog) error {
	newBundles := map[string]*v1alpha1.BundleMetadata{}

	for _, bundle := range declCfg.Bundles {
		bundleName := fmt.Sprintf("%s-%s", catalog.Name, bundle.Name)

		bundleMeta := v1alpha1.BundleMetadata{
			TypeMeta: metav1.TypeMeta{
				APIVersion: v1alpha1.GroupVersion.String(),
				Kind:       "BundleMetadata",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: bundleName,
				Labels: map[string]string{
					"catalog": catalog.Name,
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.GroupVersion.String(),
					Kind:               "Catalog",
					Name:               catalog.Name,
					UID:                catalog.UID,
					BlockOwnerDeletion: pointer.Bool(true),
					Controller:         pointer.Bool(true),
				}},
			},
			Spec: v1alpha1.BundleMetadataSpec{
				Catalog: corev1.LocalObjectReference{Name: catalog.Name},
				Package: bundle.Package,
				Image:   bundle.Image,
			},
		}

		for _, relatedImage := range bundle.RelatedImages {
			bundleMeta.Spec.RelatedImages = append(bundleMeta.Spec.RelatedImages, v1alpha1.RelatedImage{
				Name:  relatedImage.Name,
				Image: relatedImage.Image,
			})
		}

		for _, prop := range bundle.Properties {
			// skip any properties that are of type `olm.bundle.object`
			if prop.Type == "olm.bundle.object" {
				continue
			}

			bundleMeta.Spec.Properties = append(bundleMeta.Spec.Properties, v1alpha1.Property{
				Type:  prop.Type,
				Value: prop.Value,
			})
		}
		newBundles[bundleName] = &bundleMeta
	}

	var existingBundles v1alpha1.BundleMetadataList
	if err := r.List(ctx, &existingBundles); err != nil {
		return fmt.Errorf("list existing bundle metadatas: %v", err)
	}
	for i := range existingBundles.Items {
		existingBundle := existingBundles.Items[i]
		if _, ok := newBundles[existingBundle.Name]; !ok {
			if err := r.Delete(ctx, &existingBundle); err != nil {
				return fmt.Errorf("delete existing bundle metadata %q: %v", existingBundle.Name, err)
			}
		}
	}

	ordered := sets.List(sets.KeySet(newBundles))
	for _, bundleName := range ordered {
		newBundle := newBundles[bundleName]
		if err := r.Client.Patch(ctx, newBundle, client.Apply, &client.PatchOptions{Force: pointer.Bool(true), FieldManager: "catalog-controller"}); err != nil {
			return fmt.Errorf("applying bundle metadata %q: %w", newBundle.Name, err)
		}
	}
	return nil
}

// syncPackages will create a `Package` resource for each
// "olm.package" object that exists for the given catalog contents.
// `Package.Spec.Channels` is populated by filtering all "olm.channel" objects
// where the "packageName" == `Package.Name`. Returns an error if any are encountered.
func (r *CatalogReconciler) syncPackages(ctx context.Context, declCfg *declcfg.DeclarativeConfig, catalog *v1alpha1.Catalog) error {
	newPkgs := map[string]*v1alpha1.Package{}

	for _, pkg := range declCfg.Packages {
		name := fmt.Sprintf("%s-%s", catalog.Name, pkg.Name)
		var icon *v1alpha1.Icon
		if pkg.Icon != nil {
			icon = &v1alpha1.Icon{
				Data:      pkg.Icon.Data,
				MediaType: pkg.Icon.MediaType,
			}
		}
		newPkgs[name] = &v1alpha1.Package{
			TypeMeta: metav1.TypeMeta{
				APIVersion: v1alpha1.GroupVersion.String(),
				Kind:       "Package",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					"catalog": catalog.Name,
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.GroupVersion.String(),
					Kind:               "Catalog",
					Name:               catalog.Name,
					UID:                catalog.UID,
					BlockOwnerDeletion: pointer.Bool(true),
					Controller:         pointer.Bool(true),
				}},
			},
			Spec: v1alpha1.PackageSpec{
				Catalog:        corev1.LocalObjectReference{Name: catalog.Name},
				Name:           pkg.Name,
				DefaultChannel: pkg.DefaultChannel,
				Description:    pkg.Description,
				Icon:           icon,
				Channels:       []v1alpha1.PackageChannel{},
			},
		}
	}

	for _, ch := range declCfg.Channels {
		pkgName := fmt.Sprintf("%s-%s", catalog.Name, ch.Package)
		pkg, ok := newPkgs[pkgName]
		if !ok {
			return fmt.Errorf("channel %q references package %q which does not exist", ch.Name, ch.Package)
		}
		pkgChannel := v1alpha1.PackageChannel{Name: ch.Name}
		for _, entry := range ch.Entries {
			pkgChannel.Entries = append(pkgChannel.Entries, v1alpha1.ChannelEntry{
				Name:      entry.Name,
				Replaces:  entry.Replaces,
				Skips:     entry.Skips,
				SkipRange: entry.SkipRange,
			})
		}
		pkg.Spec.Channels = append(pkg.Spec.Channels, pkgChannel)
	}

	var existingPkgs v1alpha1.PackageList
	if err := r.List(ctx, &existingPkgs); err != nil {
		return fmt.Errorf("list existing packages: %v", err)
	}
	for i := range existingPkgs.Items {
		existingPkg := existingPkgs.Items[i]
		if _, ok := newPkgs[existingPkg.Name]; !ok {
			// delete existing package
			if err := r.Delete(ctx, &existingPkg); err != nil {
				return fmt.Errorf("delete existing package %q: %v", existingPkg.Name, err)
			}
		}
	}

	ordered := sets.List(sets.KeySet(newPkgs))
	for _, pkgName := range ordered {
		newPkg := newPkgs[pkgName]
		if err := r.Client.Patch(ctx, newPkg, client.Apply, &client.PatchOptions{Force: pointer.Bool(true), FieldManager: "catalog-controller"}); err != nil {
			return fmt.Errorf("applying package %q: %w", newPkg.Name, err)
		}
	}
	return nil
}

// syncCatalogMetadata will sync all of the catalog contents to `CatalogMetadata` objects
// by creating, updating and deleting the objects as necessary. Returns an
// error if any are encountered.
func (r *CatalogReconciler) syncCatalogMetadata(ctx context.Context, fsys fs.FS, catalog *v1alpha1.Catalog) error {
	newCatalogMetadataObjs := map[string]*v1alpha1.CatalogMetadata{}

	err := declcfg.WalkMetasFS(fsys, func(path string, meta *declcfg.Meta, err error) error {
		if err != nil {
			return fmt.Errorf("error in parsing catalog content files in the filesystem: %w", err)
		}

		packageOrName := meta.Package
		if packageOrName == "" {
			packageOrName = meta.Name
		}

		var objName string
		if objName, err = generateCatalogMetadataName(ctx, catalog.Name, meta); err != nil {
			return fmt.Errorf("error in generating catalog metadata name: %w", err)
		}

		catalogMetadata := &v1alpha1.CatalogMetadata{
			TypeMeta: metav1.TypeMeta{
				APIVersion: v1alpha1.GroupVersion.String(),
				Kind:       "CatalogMetadata",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: objName,
				Labels: map[string]string{
					"catalog":       catalog.Name,
					"schema":        meta.Schema,
					"package":       meta.Package,
					"name":          meta.Name,
					"packageOrName": packageOrName,
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.GroupVersion.String(),
					Kind:               "Catalog",
					Name:               catalog.Name,
					UID:                catalog.UID,
					BlockOwnerDeletion: pointer.Bool(true),
					Controller:         pointer.Bool(true),
				}},
			},
			Spec: v1alpha1.CatalogMetadataSpec{
				Catalog: corev1.LocalObjectReference{Name: catalog.Name},
				Name:    meta.Name,
				Schema:  meta.Schema,
				Package: meta.Package,
				Content: meta.Blob,
			},
		}

		newCatalogMetadataObjs[catalogMetadata.Name] = catalogMetadata

		return nil
	})
	if err != nil {
		return fmt.Errorf("unable to parse declarative config into CatalogMetadata API: %w", err)
	}

	var existingCatalogMetadataObjs v1alpha1.CatalogMetadataList
	if err := r.List(ctx, &existingCatalogMetadataObjs); err != nil {
		return fmt.Errorf("list existing catalog metadata: %v", err)
	}
	for i, existingCatalogMetadata := range existingCatalogMetadataObjs.Items {
		if _, ok := newCatalogMetadataObjs[existingCatalogMetadata.Name]; !ok {
			// delete existing catalog metadata
			err := r.Delete(ctx, &existingCatalogMetadataObjs.Items[i])
			if err != nil {
				return fmt.Errorf("delete existing catalog metadata %q: %v", existingCatalogMetadata.Name, err)
			}
		}
	}

	ordered := sets.List(sets.KeySet(newCatalogMetadataObjs))
	for _, catalogMetadataName := range ordered {
		newcatalogMetadata := newCatalogMetadataObjs[catalogMetadataName]
		if err := r.Client.Patch(ctx, newcatalogMetadata, client.Apply, &client.PatchOptions{Force: pointer.Bool(true), FieldManager: "catalog-controller"}); err != nil {
			return fmt.Errorf("applying catalog metadata %q: %w", newcatalogMetadata.Name, err)
		}
	}

	return nil
}

// generateCatalogMetadataName will generate unique names for the CatalogMetadata objects that are constructed with the
// catalog name and `meta.Schema`. Additionally, if the `meta.Package` and `meta.Name` exist, they are appended to the CatalogMetadata name.
// In the place of the empty `meta.Name`, it computes a hash of `meta.Blob` to prevent multiple FBC blobs colliding on the the object name.
// Possible outcomes are: "{catalogName}-{meta.Schema}-{meta.Name}", "{catalogName}-{meta.Schema}-{meta.Package}-{meta.Name}",
// "{catalogName}-{meta.Schema}-{hash{meta.Blob}}", "{catalogName}-{meta.Schema}-{meta.Package}-{hash{meta.Blob}}".
func generateCatalogMetadataName(_ context.Context, catalogName string, meta *declcfg.Meta) (string, error) {
	objName := fmt.Sprintf("%s-%s", catalogName, meta.Schema)
	if meta.Package != "" {
		objName = fmt.Sprintf("%s-%s", objName, meta.Package)
	}
	if meta.Name != "" {
		objName = fmt.Sprintf("%s-%s", objName, meta.Name)
	} else {
		metaJSON, err := json.Marshal(meta.Blob)
		if err != nil {
			return "", fmt.Errorf("JSON marshal error: %v", err)
		}
		// #nosec
		h := sha1.New()
		h.Write(metaJSON)
		objName = fmt.Sprintf("%s-%s", objName, hex.EncodeToString(h.Sum(nil)))
	}
	return objName, nil
}
