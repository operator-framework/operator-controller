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
	"errors"
	"fmt"
	"sort"
	"strings"

	mmsemver "github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	kappctrlv1alpha1 "github.com/vmware-tanzu/carvel-kapp-controller/pkg/apis/kappctrl/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	"github.com/operator-framework/operator-controller/pkg/features"
)

// ExtensionReconciler reconciles a Extension object
type ExtensionReconciler struct {
	client.Client
	BundleProvider BundleProvider
	HasKappApis    bool
}

var errkappAPIUnavailable = errors.New("kapp-controller apis unavailable on cluster")

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=extensions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=extensions/status,verbs=update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=extensions/finalizers,verbs=update
//+kubebuilder:rbac:groups=kappctrl.k14s.io,resources=apps,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("extension-controller")
	l.V(1).Info("starting")
	defer l.V(1).Info("ending")

	var existingExt = &ocv1alpha1.Extension{}
	if err := r.Client.Get(ctx, req.NamespacedName, existingExt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reconciledExt := existingExt.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledExt)

	// Do checks before any Update()s, as Update() may modify the resource structure!
	updateStatus := !equality.Semantic.DeepEqual(existingExt.Status, reconciledExt.Status)
	updateFinalizers := !equality.Semantic.DeepEqual(existingExt.Finalizers, reconciledExt.Finalizers)
	unexpectedFieldsChanged := r.checkForUnexpectedFieldChange(*existingExt, *reconciledExt)

	if updateStatus {
		if updateErr := r.Client.Status().Update(ctx, reconciledExt); updateErr != nil {
			return res, utilerrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}

	if unexpectedFieldsChanged {
		panic("spec or metadata changed by reconciler")
	}

	if updateFinalizers {
		if updateErr := r.Client.Update(ctx, reconciledExt); updateErr != nil {
			return res, utilerrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}

	return res, reconcileErr
}

// Compare resources - ignoring status & metadata.finalizers
func (*ExtensionReconciler) checkForUnexpectedFieldChange(a, b ocv1alpha1.Extension) bool {
	a.Status, b.Status = ocv1alpha1.ExtensionStatus{}, ocv1alpha1.ExtensionStatus{}
	a.Finalizers, b.Finalizers = []string{}, []string{}
	return !equality.Semantic.DeepEqual(a, b)
}

// Helper function to do the actual reconcile
//
// Today we always return ctrl.Result{} and an error.
// But in the future we might update this function
// to return different results (e.g. requeue).
//
//nolint:unparam
func (r *ExtensionReconciler) reconcile(ctx context.Context, ext *ocv1alpha1.Extension) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("extension-controller")

	// Don't do anything if feature gated
	if !features.OperatorControllerFeatureGate.Enabled(features.EnableExtensionAPI) {
		l.Info("extension feature is gated", "name", ext.GetName(), "namespace", ext.GetNamespace())

		// Set the TypeInstalled condition to Unknown to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "extension feature is disabled", ext.GetGeneration())
		// Set the TypeResolved condition to Unknown to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		ext.Status.ResolvedBundleResource = ""
		setResolvedStatusConditionUnknown(&ext.Status.Conditions, "extension feature is disabled", ext.GetGeneration())

		setDeprecationStatusesUnknown(&ext.Status.Conditions, "extension feature is disabled", ext.GetGeneration())
		return ctrl.Result{}, nil
	}

	// Don't do anything if Paused
	ext.Status.Paused = ext.Spec.Paused
	if ext.Spec.Paused {
		l.Info("resource is paused", "name", ext.GetName(), "namespace", ext.GetNamespace())
		return ctrl.Result{}, nil
	}

	if !r.HasKappApis {
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionFailed(&ext.Status.Conditions, errkappAPIUnavailable.Error(), ext.GetGeneration())

		ext.Status.ResolvedBundleResource = ""
		setResolvedStatusConditionUnknown(&ext.Status.Conditions, "kapp apis are unavailable", ext.GetGeneration())

		setDeprecationStatusesUnknown(&ext.Status.Conditions, "kapp apis are unavailable", ext.GetGeneration())
		return ctrl.Result{}, errkappAPIUnavailable
	}

	// TODO: Improve the resolution logic.
	bundle, err := r.resolve(ctx, *ext)
	if err != nil {
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "installation has not been attempted as resolution failed", ext.GetGeneration())
		ext.Status.ResolvedBundleResource = ""
		setResolvedStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as resolution failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// Now we can set the Resolved Condition, and the resolvedBundleSource field to the bundle.Image value.
	ext.Status.ResolvedBundleResource = bundle.Image
	setResolvedStatusConditionSuccess(&ext.Status.Conditions, fmt.Sprintf("resolved to %q", bundle.Image), ext.GetGeneration())

	mediaType, err := bundle.MediaType()
	if err != nil {
		setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// TODO: this needs to include the registryV1 bundle option. As of this PR, this only supports direct
	// installation of a set of manifests.
	if mediaType != catalogmetadata.MediaTypePlain {
		// Set the TypeInstalled condition to Unknown to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, fmt.Sprintf("bundle type %s not supported currently", mediaType), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		return ctrl.Result{}, nil
	}

	app := r.GenerateExpectedApp(*ext, bundle.Image)
	if err := r.ensureApp(ctx, app); err != nil {
		// originally Reason: ocv1alpha1.ReasonInstallationFailed
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// Converting into structured so that we can map the relevant status to Extension.
	existingTypedApp := &kappctrlv1alpha1.App{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(app.UnstructuredContent(), existingTypedApp); err != nil {
		// originally Reason: ocv1alpha1.ReasonInstallationStatusUnknown
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	mapAppStatusToInstalledCondition(existingTypedApp, ext, bundle.Image)
	SetDeprecationStatusInExtension(ext, bundle)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// TODO: Add watch for kapp-controller resources

	// When feature-gated, don't watch catalogs.
	if !features.OperatorControllerFeatureGate.Enabled(features.EnableExtensionAPI) {
		return ctrl.NewControllerManagedBy(mgr).
			For(&ocv1alpha1.Extension{}).
			Complete(r)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&ocv1alpha1.Extension{}).
		Owns(&kappctrlv1alpha1.App{}).
		Watches(&catalogd.Catalog{}, handler.EnqueueRequestsFromMapFunc(extensionRequestsForCatalog(mgr.GetClient(), mgr.GetLogger()))).
		Complete(r)
}

// TODO: follow up with mapping of all the available App statuses: https://github.com/carvel-dev/kapp-controller/blob/855063edee53315811a13ee8d5df1431ba258ede/pkg/apis/kappctrl/v1alpha1/status.go#L28-L35
// mapAppStatusToInstalledCondition currently maps only the installed condition.
func mapAppStatusToInstalledCondition(existingApp *kappctrlv1alpha1.App, ext *ocv1alpha1.Extension, bundleImage string) {
	appReady := findStatusCondition(existingApp.Status.GenericStatus.Conditions, kappctrlv1alpha1.ReconcileSucceeded)
	if appReady == nil {
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "install status unknown", ext.Generation)
		return
	}

	if appReady.Status != corev1.ConditionTrue {
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionFailed(
			&ext.Status.Conditions,
			appReady.Message,
			ext.GetGeneration(),
		)
		return
	}

	// InstalledBundleResource this should be converted into a slice as App allows fetching
	// from multiple sources.
	ext.Status.InstalledBundleResource = bundleImage
	setInstalledStatusConditionSuccess(&ext.Status.Conditions, appReady.Message, ext.Generation)
}

// setDeprecationStatus will set the appropriate deprecation statuses for a Extension
// based on the provided bundle
func SetDeprecationStatusInExtension(ext *ocv1alpha1.Extension, bundle *catalogmetadata.Bundle) {
	// reset conditions to false
	conditionTypes := []string{
		ocv1alpha1.TypeDeprecated,
		ocv1alpha1.TypePackageDeprecated,
		ocv1alpha1.TypeChannelDeprecated,
		ocv1alpha1.TypeBundleDeprecated,
	}

	for _, conditionType := range conditionTypes {
		apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               conditionType,
			Reason:             ocv1alpha1.ReasonDeprecated,
			Status:             metav1.ConditionFalse,
			Message:            "",
			ObservedGeneration: ext.Generation,
		})
	}

	// There are two early return scenarios here:
	// 1) The bundle is not deprecated (i.e bundle deprecations)
	// AND there are no other deprecations associated with the bundle
	// 2) The bundle is not deprecated, there are deprecations associated
	// with the bundle (i.e at least one channel the bundle is present in is deprecated OR whole package is deprecated),
	// and the Extension does not specify a channel. This is because the channel deprecations
	// are a loose deprecation coupling on the bundle. A Extension installation is only
	// considered deprecated by a channel deprecation when a deprecated channel is specified via
	// the spec.channel field.
	if (!bundle.IsDeprecated() && !bundle.HasDeprecation()) || (!bundle.IsDeprecated() && ext.Spec.Source.Package.Channel == "") {
		return
	}

	deprecationMessages := []string{}

	for _, deprecation := range bundle.Deprecations {
		switch deprecation.Reference.Schema {
		case declcfg.SchemaPackage:
			apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
				Type:               ocv1alpha1.TypePackageDeprecated,
				Reason:             ocv1alpha1.ReasonDeprecated,
				Status:             metav1.ConditionTrue,
				Message:            deprecation.Message,
				ObservedGeneration: ext.Generation,
			})
		case declcfg.SchemaChannel:
			if ext.Spec.Source.Package.Channel != deprecation.Reference.Name {
				continue
			}

			apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
				Type:               ocv1alpha1.TypeChannelDeprecated,
				Reason:             ocv1alpha1.ReasonDeprecated,
				Status:             metav1.ConditionTrue,
				Message:            deprecation.Message,
				ObservedGeneration: ext.Generation,
			})
		case declcfg.SchemaBundle:
			apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
				Type:               ocv1alpha1.TypeBundleDeprecated,
				Reason:             ocv1alpha1.ReasonDeprecated,
				Status:             metav1.ConditionTrue,
				Message:            deprecation.Message,
				ObservedGeneration: ext.Generation,
			})
		}

		deprecationMessages = append(deprecationMessages, deprecation.Message)
	}

	if len(deprecationMessages) > 0 {
		apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1alpha1.TypeDeprecated,
			Reason:             ocv1alpha1.ReasonDeprecated,
			Status:             metav1.ConditionTrue,
			Message:            strings.Join(deprecationMessages, ";"),
			ObservedGeneration: ext.Generation,
		})
	}
}

// findStatusCondition finds the conditionType in conditions.
// TODO: suggest using upstream conditions to Carvel.
func findStatusCondition(conditions []kappctrlv1alpha1.Condition, conditionType kappctrlv1alpha1.ConditionType) *kappctrlv1alpha1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func (r *ExtensionReconciler) ensureApp(ctx context.Context, desiredApp *unstructured.Unstructured) error {
	existingApp, err := r.existingAppUnstructured(ctx, desiredApp.GetName(), desiredApp.GetNamespace())
	if client.IgnoreNotFound(err) != nil {
		return err
	}

	// If the existing App already has everything that the desired App has, no need to contact the API server.
	// Make sure the status of the existingApp from the server is as expected.
	if equality.Semantic.DeepDerivative(desiredApp, existingApp) {
		*desiredApp = *existingApp
		return nil
	}

	return r.Client.Patch(ctx, desiredApp, client.Apply, client.ForceOwnership, client.FieldOwner("operator-controller"))
}

func (r *ExtensionReconciler) existingAppUnstructured(ctx context.Context, name, namespace string) (*unstructured.Unstructured, error) {
	existingApp := &kappctrlv1alpha1.App{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, existingApp)
	if err != nil {
		return nil, err
	}
	existingApp.APIVersion = "kappctrl.k14s.io/v1alpha1"
	existingApp.Kind = "App"
	unstrExistingAppObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(existingApp)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: unstrExistingAppObj}, nil
}

// Generate reconcile requests for all extensions affected by a catalog change
func extensionRequestsForCatalog(c client.Reader, logger logr.Logger) handler.MapFunc {
	return func(ctx context.Context, _ client.Object) []reconcile.Request {
		// no way of associating an extension to a catalog so create reconcile requests for everything
		extensions := ocv1alpha1.ExtensionList{}
		err := c.List(ctx, &extensions)
		if err != nil {
			logger.Error(err, "unable to enqueue extensions for catalog reconcile")
			return nil
		}
		var requests []reconcile.Request
		for _, ext := range extensions.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: ext.GetNamespace(),
					Name:      ext.GetName(),
				},
			})
		}
		return requests
	}
}

func (r *ExtensionReconciler) GenerateExpectedApp(o ocv1alpha1.Extension, bundlePath string) *unstructured.Unstructured {
	// We use unstructured here to avoid problems of serializing default values when sending patches to the apiserver.
	// If you use a typed object, any default values from that struct get serialized into the JSON patch, which could
	// cause unrelated fields to be patched back to the default value even though that isn't the intention. Using an
	// unstructured ensures that the patch contains only what is specified. Using unstructured like this is basically
	// identical to "kubectl apply -f"
	spec := map[string]interface{}{
		"serviceAccountName": o.Spec.ServiceAccountName,
		"fetch": []interface{}{
			map[string]interface{}{
				"image": map[string]interface{}{
					"url": bundlePath,
				},
			},
		},
		"template": []interface{}{
			map[string]interface{}{
				"ytt": map[string]interface{}{},
			},
		},
		"deploy": []interface{}{
			map[string]interface{}{
				"kapp": map[string]interface{}{},
			},
		},
	}

	app := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kappctrl.k14s.io/v1alpha1",
			"kind":       "App",
			"metadata": map[string]interface{}{
				"name":      o.GetName(),
				"namespace": o.GetNamespace(),
			},
			"spec": spec,
		},
	}

	app.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         ocv1alpha1.GroupVersion.String(),
			Kind:               "Extension",
			Name:               o.Name,
			UID:                o.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	})
	return app
}

func (r *ExtensionReconciler) resolve(ctx context.Context, extension ocv1alpha1.Extension) (*catalogmetadata.Bundle, error) {
	allBundles, err := r.BundleProvider.Bundles(ctx)
	if err != nil {
		return nil, err
	}

	packageName := extension.Spec.Source.Package.Name
	channelName := extension.Spec.Source.Package.Channel
	versionRange := extension.Spec.Source.Package.Version

	predicates := []catalogfilter.Predicate[catalogmetadata.Bundle]{
		catalogfilter.WithPackageName(packageName),
	}

	if channelName != "" {
		predicates = append(predicates, catalogfilter.InChannel(channelName))
	}

	if versionRange != "" {
		vr, err := mmsemver.NewConstraint(versionRange)
		if err != nil {
			return nil, fmt.Errorf("invalid version range %q: %w", versionRange, err)
		}
		predicates = append(predicates, catalogfilter.InMastermindsSemverRange(vr))
	}

	resultSet := catalogfilter.Filter(allBundles, catalogfilter.And(predicates...))

	if len(resultSet) == 0 {
		if versionRange != "" && channelName != "" {
			return nil, fmt.Errorf("no package %q matching version %q found in channel %q", packageName, versionRange, channelName)
		}
		if versionRange != "" {
			return nil, fmt.Errorf("no package %q matching version %q found", packageName, versionRange)
		}
		if channelName != "" {
			return nil, fmt.Errorf("no package %q found in channel %q", packageName, channelName)
		}
		return nil, fmt.Errorf("no package %q found", packageName)
	}

	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByVersion(resultSet[i], resultSet[j])
	})
	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByDeprecated(resultSet[i], resultSet[j])
	})
	return resultSet[0], nil
}
