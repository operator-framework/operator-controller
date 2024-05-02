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
	"sort"
	"strings"

	mmsemver "github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
)

// ClusterExtensionReconciler reconciles a ClusterExtension object
type ClusterExtensionReconciler struct {
	client.Client
	BundleProvider BundleProvider
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions,verbs=get;list;watch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions/status,verbs=update;patch
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
	// Lookup the bundle that corresponds to the ClusterExtension's desired package.
	bundle, err := r.resolve(ctx, ext)
	if err != nil {
		ext.Status.InstalledBundle = nil
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "installation has not been attempted as resolution failed", ext.GetGeneration())
		ext.Status.ResolvedBundle = nil
		setResolvedStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())

		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as resolution failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// Now we can set the Resolved Condition, and the resolvedBundleSource field to the bundle.Image value.
	ext.Status.ResolvedBundle = bundleMetadataFor(bundle)
	setResolvedStatusConditionSuccess(&ext.Status.Conditions, fmt.Sprintf("resolved to %q", bundle.Image), ext.GetGeneration())

	// TODO: Question - Should we set the deprecation statuses after we have successfully resolved instead of after a successful installation?

	mediaType, err := bundle.MediaType()
	if err != nil {
		setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	if err := r.validateBundle(bundle); err != nil {
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
		ext.Status.InstalledBundle = nil
		setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// convert existing unstructured object into bundleDeployment for easier mapping of status.
	existingTypedBundleDeployment := &rukpakv1alpha2.BundleDeployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(dep.UnstructuredContent(), existingTypedBundleDeployment); err != nil {
		// originally Reason: ocv1alpha1.ReasonInstallationStatusUnknown
		ext.Status.InstalledBundle = nil
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}

	// Let's set the proper Installed condition and InstalledBundle field based on the
	// existing BundleDeployment object status.
	mapBDStatusToInstalledCondition(existingTypedBundleDeployment, ext, bundle)

	SetDeprecationStatus(ext, bundle)

	// set the status of the cluster extension based on the respective bundle deployment status conditions.
	return ctrl.Result{}, nil
}

func (r *ClusterExtensionReconciler) resolve(ctx context.Context, ext *ocv1alpha1.ClusterExtension) (*catalogmetadata.Bundle, error) {
	allBundles, err := r.BundleProvider.Bundles(ctx)
	if err != nil {
		return nil, err
	}

	installedBundle, err := r.installedBundle(ctx, allBundles, ext)
	if err != nil {
		return nil, err
	}

	packageName := ext.Spec.PackageName
	channelName := ext.Spec.Channel
	versionRange := ext.Spec.Version

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

	if ext.Spec.UpgradeConstraintPolicy != ocv1alpha1.UpgradeConstraintPolicyIgnore && installedBundle != nil {
		upgradePredicate, err := SuccessorsPredicate(installedBundle)
		if err != nil {
			return nil, err
		}

		predicates = append(predicates, upgradePredicate)
	}

	resultSet := catalogfilter.Filter(allBundles, catalogfilter.And(predicates...))

	var upgradeErrorPrefix string
	if installedBundle != nil {
		installedBundleVersion, err := installedBundle.Version()
		if err != nil {
			return nil, err
		}
		upgradeErrorPrefix = fmt.Sprintf("error upgrading from currently installed version %q: ", installedBundleVersion.String())
	}
	if len(resultSet) == 0 {
		if versionRange != "" && channelName != "" {
			return nil, fmt.Errorf("%sno package %q matching version %q found in channel %q", upgradeErrorPrefix, packageName, versionRange, channelName)
		}
		if versionRange != "" {
			return nil, fmt.Errorf("%sno package %q matching version %q found", upgradeErrorPrefix, packageName, versionRange)
		}
		if channelName != "" {
			return nil, fmt.Errorf("%sno package %q found in channel %q", upgradeErrorPrefix, packageName, channelName)
		}
		return nil, fmt.Errorf("%sno package %q found", upgradeErrorPrefix, packageName)
	}
	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByVersion(resultSet[i], resultSet[j])
	})
	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByDeprecated(resultSet[i], resultSet[j])
	})

	return resultSet[0], nil
}

func (r *ClusterExtensionReconciler) installedBundle(ctx context.Context, allBundles []*catalogmetadata.Bundle, ext *ocv1alpha1.ClusterExtension) (*catalogmetadata.Bundle, error) {
	bd := &rukpakv1alpha2.BundleDeployment{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: ext.GetName()}, bd)
	if client.IgnoreNotFound(err) != nil {
		return nil, err
	}

	if bd.Spec.Source.Image == nil || bd.Spec.Source.Image.Ref == "" {
		// Bundle not yet installed
		return nil, nil
	}

	bundleImage := bd.Spec.Source.Image.Ref
	// find corresponding bundle for the installed content
	resultSet := catalogfilter.Filter(allBundles, catalogfilter.And(
		catalogfilter.WithPackageName(ext.Spec.PackageName),
		catalogfilter.WithBundleImage(bundleImage),
	))
	if len(resultSet) == 0 {
		return nil, fmt.Errorf("bundle with image %q for package %q not found in available catalogs but is currently installed via BundleDeployment %q", bundleImage, ext.Spec.PackageName, bd.Name)
	}

	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByVersion(resultSet[i], resultSet[j])
	})

	return resultSet[0], nil
}

func (r *ClusterExtensionReconciler) validateBundle(bundle *catalogmetadata.Bundle) error {
	unsupportedProps := sets.New(
		property.TypePackageRequired,
		property.TypeGVKRequired,
		property.TypeConstraint,
	)
	for i := range bundle.Properties {
		if unsupportedProps.Has(bundle.Properties[i].Type) {
			return fmt.Errorf(
				"bundle %q has a dependency declared via property %q which is currently not supported",
				bundle.Name,
				bundle.Properties[i].Type,
			)
		}
	}

	return nil
}

func mapBDStatusToInstalledCondition(existingTypedBundleDeployment *rukpakv1alpha2.BundleDeployment, ext *ocv1alpha1.ClusterExtension, bundle *catalogmetadata.Bundle) {
	bundleDeploymentReady := apimeta.FindStatusCondition(existingTypedBundleDeployment.Status.Conditions, rukpakv1alpha2.TypeInstalled)
	if bundleDeploymentReady == nil {
		ext.Status.InstalledBundle = nil
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "bundledeployment status is unknown", ext.GetGeneration())
		return
	}

	if bundleDeploymentReady.Status != metav1.ConditionTrue {
		ext.Status.InstalledBundle = nil
		setInstalledStatusConditionFailed(
			&ext.Status.Conditions,
			fmt.Sprintf("bundledeployment not ready: %s", bundleDeploymentReady.Message),
			ext.GetGeneration(),
		)
		return
	}

	installedBundle := bundleMetadataFor(bundle)
	bundleDeploymentSource := existingTypedBundleDeployment.Spec.Source
	switch bundleDeploymentSource.Type {
	case rukpakv1alpha2.SourceTypeImage:
		ext.Status.InstalledBundle = installedBundle
		setInstalledStatusConditionSuccess(
			&ext.Status.Conditions,
			fmt.Sprintf("installed from %q", bundleDeploymentSource.Image.Ref),
			ext.GetGeneration(),
		)
	case rukpakv1alpha2.SourceTypeGit:
		ext.Status.InstalledBundle = installedBundle
		resource := bundleDeploymentSource.Git.Repository + "@" + bundleDeploymentSource.Git.Ref.Commit
		setInstalledStatusConditionSuccess(
			&ext.Status.Conditions,
			fmt.Sprintf("installed from %q", resource),
			ext.GetGeneration(),
		)
	default:
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

func (r *ClusterExtensionReconciler) GenerateExpectedBundleDeployment(o ocv1alpha1.ClusterExtension, bundlePath string, bundleProvisioner string) *unstructured.Unstructured {
	// We use unstructured here to avoid problems of serializing default values when sending patches to the apiserver.
	// If you use a typed object, any default values from that struct get serialized into the JSON patch, which could
	// cause unrelated fields to be patched back to the default value even though that isn't the intention. Using an
	// unstructured ensures that the patch contains only what is specified. Using unstructured like this is basically
	// identical to "kubectl apply -f"

	spec := map[string]interface{}{
		"installNamespace":     o.Spec.InstallNamespace,
		"provisionerClassName": bundleProvisioner,
		"source": map[string]interface{}{
			// TODO: Don't assume image type
			"type": string(rukpakv1alpha2.SourceTypeImage),
			"image": map[string]interface{}{
				"ref": bundlePath,
			},
		},
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
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
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
