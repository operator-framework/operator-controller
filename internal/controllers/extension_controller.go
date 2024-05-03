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
	"sort"
	"strings"

	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"
	"github.com/go-logr/logr"
	kappctrlv1alpha1 "github.com/vmware-tanzu/carvel-kapp-controller/pkg/apis/kappctrl/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	"github.com/operator-framework/operator-controller/pkg/features"
)

// ExtensionReconciler reconciles a Extension object
type ExtensionReconciler struct {
	client.Client
	BundleProvider BundleProvider
}

var (
	bundleVersionKey = "olm.operatorframework.io/bundleVersion"
)

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

		// Set the TypeInstalled condition to Failed to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		ext.Status.InstalledBundle = nil
		setInstalledStatusConditionFailed(&ext.Status.Conditions, "extension feature is disabled", ext.GetGeneration())
		// Set the TypeResolved condition to Failed to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		ext.Status.ResolvedBundle = nil
		setResolvedStatusConditionFailed(&ext.Status.Conditions, "extension feature is disabled", ext.GetGeneration())

		setDeprecationStatusesUnknown(&ext.Status.Conditions, "extension feature is disabled", ext.GetGeneration())
		return ctrl.Result{}, nil
	}

	// Don't do anything if Paused
	ext.Status.Paused = ext.Spec.Paused
	if ext.Spec.Paused {
		l.Info("resource is paused", "name", ext.GetName(), "namespace", ext.GetNamespace())
		return ctrl.Result{}, nil
	}

	// TODO: Improve the resolution logic.
	bundle, err := r.resolve(ctx, *ext)
	if err != nil {
		if c := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeInstalled); c == nil {
			ext.Status.InstalledBundle = nil
			setInstalledStatusConditionFailed(&ext.Status.Conditions, "installation has not been attempted as resolution failed", ext.GetGeneration())
		}
		ext.Status.ResolvedBundle = nil
		setResolvedStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as resolution failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// Now we can set the Resolved Condition, and the resolvedBundleSource field to the bundle.Image value.
	ext.Status.ResolvedBundle = bundleMetadataFor(bundle)
	setResolvedStatusConditionSuccess(&ext.Status.Conditions, fmt.Sprintf("resolved to %q", bundle.Image), ext.GetGeneration())

	// Right now, we just assume that the bundle is a plain+v0 bundle.
	app, err := r.GenerateExpectedApp(*ext, bundle)
	if err != nil {
		if c := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeInstalled); c == nil {
			ext.Status.InstalledBundle = nil
			setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
			setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		}
		setProgressingStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		return ctrl.Result{}, err
	}

	if err := r.ensureApp(ctx, app); err != nil {
		// originally Reason: ocv1alpha1.ReasonInstallationFailed
		ext.Status.InstalledBundle = nil
		setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		setProgressingStatusConditionProgressing(&ext.Status.Conditions, "installation failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// Converting into structured so that we can map the relevant status to Extension.
	existingTypedApp := &kappctrlv1alpha1.App{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(app.UnstructuredContent(), existingTypedApp); err != nil {
		// originally Reason: ocv1alpha1.ReasonInstallationStatusUnknown
		ext.Status.InstalledBundle = nil
		setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		setProgressingStatusConditionProgressing(&ext.Status.Conditions, "installation failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	ext.Status.InstalledBundle = bundleMetadataFor(bundle)
	setInstalledStatusConditionSuccess(&ext.Status.Conditions, fmt.Sprintf("successfully installed %v", ext.Status.InstalledBundle), ext.GetGeneration())
	SetDeprecationStatusInExtension(ext, bundle)

	// TODO: add conditions to determine extension health
	mapAppStatusToCondition(existingTypedApp, ext)

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

// mapAppStatusToCondition maps the reconciling/deleting App conditions to the installed/deleting conditions on the Extension.
func mapAppStatusToCondition(existingApp *kappctrlv1alpha1.App, ext *ocv1alpha1.Extension) {
	// Note: App.Status.Inspect errors are never surfaced to App conditions, so are currently ignored when determining App status.
	if ext == nil || existingApp == nil {
		return
	}
	message := existingApp.Status.FriendlyDescription
	if len(message) == 0 || strings.Contains(message, "Error (see .status.usefulErrorMessage for details)") {
		message = existingApp.Status.UsefulErrorMessage
	}

	appStatusMapFn := map[kappctrlv1alpha1.ConditionType]func(*[]metav1.Condition, string, int64){
		kappctrlv1alpha1.Deleting:           setProgressingStatusConditionProgressing,
		kappctrlv1alpha1.Reconciling:        setProgressingStatusConditionProgressing,
		kappctrlv1alpha1.DeleteFailed:       setProgressingStatusConditionFailed,
		kappctrlv1alpha1.ReconcileFailed:    setProgressingStatusConditionFailed,
		kappctrlv1alpha1.ReconcileSucceeded: setProgressingStatusConditionSuccess,
	}
	for cond := range appStatusMapFn {
		if c := findStatusCondition(existingApp.Status.GenericStatus.Conditions, cond); c != nil && c.Status == corev1.ConditionTrue {
			if len(message) == 0 {
				message = c.Message
			}
			appStatusMapFn[cond](&ext.Status.Conditions, fmt.Sprintf("App %s: %s", c.Type, message), ext.Generation)
			return
		}
	}
	if len(message) == 0 {
		message = "waiting for app"
	}
	setProgressingStatusConditionProgressing(&ext.Status.Conditions, message, ext.Generation)
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

func (r *ExtensionReconciler) GenerateExpectedApp(o ocv1alpha1.Extension, bundle *catalogmetadata.Bundle) (*unstructured.Unstructured, error) {
	bundleVersion, err := bundle.Version()
	if err != nil {
		return nil, fmt.Errorf("failed to generate App from Extension %q with bundle %q: %w", o.GetName(), bundle.Name, err)
	}
	bundlePath := bundle.Image

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
				"annotations": map[string]string{
					bundleVersionKey: bundleVersion.String(),
				},
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
	return app, nil
}

func (r *ExtensionReconciler) getInstalledVersion(ctx context.Context, namespacedName types.NamespacedName) (*bsemver.Version, error) {
	existingApp, err := r.existingAppUnstructured(ctx, namespacedName.Name, namespacedName.Namespace)
	if err != nil {
		return nil, err
	}
	existingVersion, ok := existingApp.GetAnnotations()[bundleVersionKey]
	if !ok {
		return nil, fmt.Errorf("existing App %q in Namespace %q missing bundle version", namespacedName.Name, namespacedName.Namespace)
	}

	existingVersionSemver, err := bsemver.New(existingVersion)
	if err != nil {
		return nil, fmt.Errorf("could not determine bundle version of existing App %q in Namespace %q: %w", namespacedName.Name, namespacedName.Namespace, err)
	}
	return existingVersionSemver, nil
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

	var installedVersion string
	// Do not include bundle versions older than currently installed unless UpgradeConstraintPolicy = 'Ignore'
	if extension.Spec.Source.Package.UpgradeConstraintPolicy != ocv1alpha1.UpgradeConstraintPolicyIgnore {
		installedVersionSemver, err := r.getInstalledVersion(ctx, types.NamespacedName{Name: extension.GetName(), Namespace: extension.GetNamespace()})
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		}
		if installedVersionSemver != nil {
			installedVersion = installedVersionSemver.String()

			// Based on installed version create a caret range comparison constraint
			// to allow only minor and patch version as successors.
			wantedVersionRangeConstraint, err := mmsemver.NewConstraint(fmt.Sprintf("^%s", installedVersion))
			if err != nil {
				return nil, err
			}
			predicates = append(predicates, catalogfilter.InMastermindsSemverRange(wantedVersionRangeConstraint))
		}
	}

	resultSet := catalogfilter.Filter(allBundles, catalogfilter.And(predicates...))

	if len(resultSet) == 0 {
		var versionError, channelError, existingVersionError string
		if versionRange != "" {
			versionError = fmt.Sprintf(" matching version %q", versionRange)
		}
		if channelName != "" {
			channelError = fmt.Sprintf(" in channel %q", channelName)
		}
		if installedVersion != "" {
			existingVersionError = fmt.Sprintf(" which upgrades currently installed version %q", installedVersion)
		}
		return nil, fmt.Errorf("no package %q%s%s%s found", packageName, versionError, channelError, existingVersionError)
	}

	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByVersion(resultSet[i], resultSet[j])
	})
	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByDeprecated(resultSet[i], resultSet[j])
	})
	return resultSet[0], nil
}

// bundleMetadataFor returns a BundleMetadata for the given bundle. If the provided bundle is nil,
// this function panics. It is up to the caller to ensure that the bundle is non-nil.
func bundleMetadataFor(bundle *catalogmetadata.Bundle) *ocv1alpha1.BundleMetadata {
	if bundle == nil {
		panic("programmer error: provided bundle must be non-nil to create BundleMetadata")
	}
	ver, err := bundle.Version()
	if err != nil {
		ver = &bsemver.Version{}
	}
	return &ocv1alpha1.BundleMetadata{
		Name:    bundle.Name,
		Version: ver.String(),
	}
}
