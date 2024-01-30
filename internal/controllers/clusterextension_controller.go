/*
Copyright 2023.

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
	"strings"

	"github.com/go-logr/logr"
	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/solver"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/controllers/validators"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

// ClusterExtensionReconciler reconciles a ClusterExtension object
type ClusterExtensionReconciler struct {
	client.Client
	BundleProvider BundleProvider
	Scheme         *runtime.Scheme
	Resolver       *solver.Solver
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions,verbs=get;list;watch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions/finalizers,verbs=update

//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundledeployments,verbs=get;list;watch;create;update;patch

//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogs,verbs=list;watch
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogmetadata,verbs=list;watch

func (r *ClusterExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("operator-controller")
	l.V(1).Info("starting")
	defer l.V(1).Info("ending")

	var existingExt = &ocv1alpha1.ClusterExtension{}
	if err := r.Get(ctx, req.NamespacedName, existingExt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reconciledExt := existingExt.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledExt)

	// Do checks before any Update()s, as Update() may modify the resource structure!
	updateStatus := !equality.Semantic.DeepEqual(existingExt.Status, reconciledExt.Status)
	updateFinalizers := !equality.Semantic.DeepEqual(existingExt.Finalizers, reconciledExt.Finalizers)
	unexpectedFieldsChanged := checkForUnexpectedFieldChange(*existingExt, *reconciledExt)

	if updateStatus {
		if updateErr := r.Status().Update(ctx, reconciledExt); updateErr != nil {
			return res, utilerrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}

	if unexpectedFieldsChanged {
		panic("spec or metadata changed by reconciler")
	}

	if updateFinalizers {
		if updateErr := r.Update(ctx, reconciledExt); updateErr != nil {
			return res, utilerrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}

	return res, reconcileErr
}

// Compare resources - ignoring status & metadata.finalizers
func checkForUnexpectedFieldChange(a, b ocv1alpha1.ClusterExtension) bool {
	a.Status, b.Status = ocv1alpha1.ClusterExtensionStatus{}, ocv1alpha1.ClusterExtensionStatus{}
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
func (r *ClusterExtensionReconciler) reconcile(ctx context.Context, ext *ocv1alpha1.ClusterExtension) (ctrl.Result, error) {
	// validate spec
	if err := validators.ValidateClusterExtensionSpec(ext); err != nil {
		// Set the TypeInstalled condition to Unknown to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "installation has not been attempted as spec is invalid", ext.GetGeneration())
		// Set the TypeResolved condition to Unknown to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		ext.Status.ResolvedBundleResource = ""
		setResolvedStatusConditionUnknown(&ext.Status.Conditions, "validation has not been attempted as spec is invalid", ext.GetGeneration())

		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as spec is invalid", ext.GetGeneration())
		return ctrl.Result{}, nil
	}

	// gather vars for resolution
	vars, err := r.variables(ctx)
	if err != nil {
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "installation has not been attempted due to failure to gather data for resolution", ext.GetGeneration())
		ext.Status.ResolvedBundleResource = ""
		setResolvedStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())

		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted due to failure to gather data for resolution", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// run resolution
	selection, err := r.Resolver.Solve(vars)
	if err != nil {
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "installation has not been attempted as resolution failed", ext.GetGeneration())
		ext.Status.ResolvedBundleResource = ""
		setResolvedStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())

		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as resolution failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// lookup the bundle in the solution that corresponds to the
	// ClusterExtension's desired package name.
	bundle, err := r.bundleFromSolution(selection, ext.Spec.PackageName)
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

	// TODO: Question - Should we set the deprecation statuses after we have successfully resolved instead of after a successful installation?

	mediaType, err := bundle.MediaType()
	if err != nil {
		setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}
	bundleProvisioner, err := mapBundleMediaTypeToBundleProvisioner(mediaType)
	if err != nil {
		setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}
	// Ensure a BundleDeployment exists with its bundle source from the bundle
	// image we just looked up in the solution.
	dep := r.GenerateExpectedBundleDeployment(*ext, bundle.Image, bundleProvisioner)
	if err := r.ensureBundleDeployment(ctx, dep); err != nil {
		// originally Reason: ocv1alpha1.ReasonInstallationFailed
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// convert existing unstructured object into bundleDeployment for easier mapping of status.
	existingTypedBundleDeployment := &rukpakv1alpha2.BundleDeployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(dep.UnstructuredContent(), existingTypedBundleDeployment); err != nil {
		// originally Reason: ocv1alpha1.ReasonInstallationStatusUnknown
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// Let's set the proper Installed condition and InstalledBundleResource field based on the
	// existing BundleDeployment object status.
	mapBDStatusToInstalledCondition(existingTypedBundleDeployment, ext)

	SetDeprecationStatus(ext, bundle)

	// set the status of the cluster extension based on the respective bundle deployment status conditions.
	return ctrl.Result{}, nil
}

func (r *ClusterExtensionReconciler) variables(ctx context.Context) ([]deppy.Variable, error) {
	allBundles, err := r.BundleProvider.Bundles(ctx)
	if err != nil {
		return nil, err
	}
	clusterExtensionList := ocv1alpha1.ClusterExtensionList{}
	if err := r.Client.List(ctx, &clusterExtensionList); err != nil {
		return nil, err
	}
	bundleDeploymentList := rukpakv1alpha2.BundleDeploymentList{}
	if err := r.Client.List(ctx, &bundleDeploymentList); err != nil {
		return nil, err
	}

	return GenerateVariables(allBundles, clusterExtensionList.Items, bundleDeploymentList.Items)
}

func mapBDStatusToInstalledCondition(existingTypedBundleDeployment *rukpakv1alpha2.BundleDeployment, ext *ocv1alpha1.ClusterExtension) {
	bundleDeploymentReady := apimeta.FindStatusCondition(existingTypedBundleDeployment.Status.Conditions, rukpakv1alpha2.TypeInstalled)
	if bundleDeploymentReady == nil {
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "bundledeployment status is unknown", ext.GetGeneration())
		return
	}

	if bundleDeploymentReady.Status != metav1.ConditionTrue {
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionFailed(
			&ext.Status.Conditions,
			fmt.Sprintf("bundledeployment not ready: %s", bundleDeploymentReady.Message),
			ext.GetGeneration(),
		)
		return
	}

	bundleDeploymentSource := existingTypedBundleDeployment.Spec.Source
	switch bundleDeploymentSource.Type {
	case rukpakv1alpha2.SourceTypeImage:
		ext.Status.InstalledBundleResource = bundleDeploymentSource.Image.Ref
		setInstalledStatusConditionSuccess(
			&ext.Status.Conditions,
			fmt.Sprintf("installed from %q", bundleDeploymentSource.Image.Ref),
			ext.GetGeneration(),
		)
	case rukpakv1alpha2.SourceTypeGit:
		resource := bundleDeploymentSource.Git.Repository + "@" + bundleDeploymentSource.Git.Ref.Commit
		ext.Status.InstalledBundleResource = resource
		setInstalledStatusConditionSuccess(
			&ext.Status.Conditions,
			fmt.Sprintf("installed from %q", resource),
			ext.GetGeneration(),
		)
	default:
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(
			&ext.Status.Conditions,
			fmt.Sprintf("unknown bundledeployment source type %q", bundleDeploymentSource.Type),
			ext.GetGeneration(),
		)
	}
}

// setDeprecationStatus will set the appropriate deprecation statuses for a ClusterExtension
// based on the provided bundle
func SetDeprecationStatus(ext *ocv1alpha1.ClusterExtension, bundle *catalogmetadata.Bundle) {
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
	// and the ClusterExtension does not specify a channel. This is because the channel deprecations
	// are a loose deprecation coupling on the bundle. A ClusterExtension installation is only
	// considered deprecated by a channel deprecation when a deprecated channel is specified via
	// the spec.channel field.
	if (!bundle.IsDeprecated() && !bundle.HasDeprecation()) || (!bundle.IsDeprecated() && ext.Spec.Channel == "") {
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
			if ext.Spec.Channel != deprecation.Reference.Name {
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

func (r *ClusterExtensionReconciler) bundleFromSolution(selection []deppy.Variable, packageName string) (*catalogmetadata.Bundle, error) {
	for _, variable := range selection {
		switch v := variable.(type) {
		case *olmvariables.BundleVariable:
			bundlePkgName := v.Bundle().Package
			if packageName == bundlePkgName {
				return v.Bundle(), nil
			}
		}
	}
	return nil, fmt.Errorf("bundle for package %q not found in solution", packageName)
}

func (r *ClusterExtensionReconciler) GenerateExpectedBundleDeployment(o ocv1alpha1.ClusterExtension, bundlePath string, bundleProvisioner string) *unstructured.Unstructured {
	// We use unstructured here to avoid problems of serializing default values when sending patches to the apiserver.
	// If you use a typed object, any default values from that struct get serialized into the JSON patch, which could
	// cause unrelated fields to be patched back to the default value even though that isn't the intention. Using an
	// unstructured ensures that the patch contains only what is specified. Using unstructured like this is basically
	// identical to "kubectl apply -f"

	spec := map[string]interface{}{
		// TODO: Don't assume plain provisioner
		"provisionerClassName": bundleProvisioner,
		"source": map[string]interface{}{
			// TODO: Don't assume image type
			"type": string(rukpakv1alpha2.SourceTypeImage),
			"image": map[string]interface{}{
				"ref": bundlePath,
			},
		},
	}

	if len(o.Spec.WatchNamespaces) > 0 {
		spec["watchNamespaces"] = o.Spec.WatchNamespaces
	}

	bd := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": rukpakv1alpha2.GroupVersion.String(),
		"kind":       rukpakv1alpha2.BundleDeploymentKind,
		"metadata": map[string]interface{}{
			"name": o.GetName(),
		},
		"spec": spec,
	}}
	bd.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         ocv1alpha1.GroupVersion.String(),
			Kind:               "ClusterExtension",
			Name:               o.Name,
			UID:                o.UID,
			Controller:         pointer.Bool(true),
			BlockOwnerDeletion: pointer.Bool(true),
		},
	})
	return bd
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&ocv1alpha1.ClusterExtension{}).
		Watches(&catalogd.Catalog{},
			handler.EnqueueRequestsFromMapFunc(clusterExtensionRequestsForCatalog(mgr.GetClient(), mgr.GetLogger()))).
		Owns(&rukpakv1alpha2.BundleDeployment{}).
		Complete(r)

	if err != nil {
		return err
	}
	return nil
}

func (r *ClusterExtensionReconciler) ensureBundleDeployment(ctx context.Context, desiredBundleDeployment *unstructured.Unstructured) error {
	// TODO: what if there happens to be an unrelated BD with the same name as the ClusterExtension?
	//   we should probably also check to see if there's an owner reference and/or a label set
	//   that we expect only to ever be used by the operator-controller. That way, we don't
	//   automatically and silently adopt and change a BD that the user doens't intend to be
	//   owned by the ClusterExtension.
	existingBundleDeployment, err := r.existingBundleDeploymentUnstructured(ctx, desiredBundleDeployment.GetName())
	if client.IgnoreNotFound(err) != nil {
		return err
	}

	// If the existing BD already has everything that the desired BD has, no need to contact the API server.
	// Make sure the status of the existingBD from the server is as expected.
	if equality.Semantic.DeepDerivative(desiredBundleDeployment, existingBundleDeployment) {
		*desiredBundleDeployment = *existingBundleDeployment
		return nil
	}

	return r.Client.Patch(ctx, desiredBundleDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("operator-controller"))
}

func (r *ClusterExtensionReconciler) existingBundleDeploymentUnstructured(ctx context.Context, name string) (*unstructured.Unstructured, error) {
	existingBundleDeployment := &rukpakv1alpha2.BundleDeployment{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name}, existingBundleDeployment)
	if err != nil {
		return nil, err
	}
	existingBundleDeployment.APIVersion = rukpakv1alpha2.GroupVersion.String()
	existingBundleDeployment.Kind = rukpakv1alpha2.BundleDeploymentKind
	unstrExistingBundleDeploymentObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(existingBundleDeployment)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: unstrExistingBundleDeploymentObj}, nil
}

// mapBundleMediaTypeToBundleProvisioner maps an olm.bundle.mediatype property to a
// rukpak bundle provisioner class name that is capable of unpacking the bundle type
func mapBundleMediaTypeToBundleProvisioner(mediaType string) (string, error) {
	switch mediaType {
	case catalogmetadata.MediaTypePlain:
		return "core-rukpak-io-plain", nil
	// To ensure compatibility with bundles created with OLMv0 where the
	// olm.bundle.mediatype property doesn't exist, we assume that if the
	// property is empty (i.e doesn't exist) that the bundle is one created
	// with OLMv0 and therefore should use the registry provisioner
	case catalogmetadata.MediaTypeRegistry, "":
		return "core-rukpak-io-registry", nil
	default:
		return "", fmt.Errorf("unknown bundle mediatype: %s", mediaType)
	}
}

// Generate reconcile requests for all cluster extensions affected by a catalog change
func clusterExtensionRequestsForCatalog(c client.Reader, logger logr.Logger) handler.MapFunc {
	return func(ctx context.Context, _ client.Object) []reconcile.Request {
		// no way of associating an extension to a catalog so create reconcile requests for everything
		clusterExtensions := ocv1alpha1.ClusterExtensionList{}
		err := c.List(ctx, &clusterExtensions)
		if err != nil {
			logger.Error(err, "unable to enqueue cluster extensions for catalog reconcile")
			return nil
		}
		var requests []reconcile.Request
		for _, ext := range clusterExtensions.Items {
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
