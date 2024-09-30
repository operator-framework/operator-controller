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
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	crhandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/bundleutil"
	"github.com/operator-framework/operator-controller/internal/conditionsets"
	"github.com/operator-framework/operator-controller/internal/contentmanager"
	"github.com/operator-framework/operator-controller/internal/labels"
	"github.com/operator-framework/operator-controller/internal/resolve"
	rukpaksource "github.com/operator-framework/operator-controller/internal/rukpak/source"
)

const (
	ClusterExtensionCleanupUnpackCacheFinalizer         = "olm.operatorframework.io/cleanup-unpack-cache"
	ClusterExtensionCleanupContentManagerCacheFinalizer = "olm.operatorframework.io/cleanup-contentmanager-cache"
)

// ClusterExtensionReconciler reconciles a ClusterExtension object
type ClusterExtensionReconciler struct {
	client.Client
	Resolver              resolve.Resolver
	Unpacker              rukpaksource.Unpacker
	Applier               Applier
	Manager               contentmanager.Manager
	controller            crcontroller.Controller
	cache                 cache.Cache
	InstalledBundleGetter InstalledBundleGetter
	Finalizers            crfinalizer.Finalizers
}

type Applier interface {
	// Apply applies the content in the provided fs.FS using the configuration of the provided ClusterExtension.
	// It also takes in a map[string]string to be applied to all applied resources as labels and another
	// map[string]string used to create a unique identifier for a stored reference to the resources created.
	Apply(context.Context, fs.FS, *ocv1alpha1.ClusterExtension, map[string]string, map[string]string) ([]client.Object, string, error)
}

type InstalledBundleGetter interface {
	GetInstalledBundle(ctx context.Context, ext *ocv1alpha1.ClusterExtension) (*ocv1alpha1.BundleMetadata, error)
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions/status,verbs=update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions/finalizers,verbs=update
//+kubebuilder:rbac:namespace=system,groups=core,resources=secrets,verbs=create;update;patch;delete;deletecollection;get;list;watch
//+kubebuilder:rbac:groups=core,resources=serviceaccounts/token,verbs=create
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clustercatalogs,verbs=list;watch

// The operator controller needs to watch all the bundle objects and reconcile accordingly. Though not ideal, but these permissions are required.
// This has been taken from rukpak, and an issue was created before to discuss it: https://github.com/operator-framework/rukpak/issues/800.
func (r *ClusterExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("operator-controller")
	ctx = log.IntoContext(ctx, l)

	l.Info("reconcile starting")
	defer l.Info("reconcile ending")

	existingExt := &ocv1alpha1.ClusterExtension{}
	if err := r.Client.Get(ctx, req.NamespacedName, existingExt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reconciledExt := existingExt.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledExt)

	// Do checks before any Update()s, as Update() may modify the resource structure!
	updateStatus := !equality.Semantic.DeepEqual(existingExt.Status, reconciledExt.Status)
	updateFinalizers := !equality.Semantic.DeepEqual(existingExt.Finalizers, reconciledExt.Finalizers)

	// If any unexpected fields have changed, panic before updating the resource
	unexpectedFieldsChanged := checkForUnexpectedFieldChange(*existingExt, *reconciledExt)
	if unexpectedFieldsChanged {
		panic("spec or metadata changed by reconciler")
	}

	// Save the finalizers off to the side. If we update the status, the reconciledExt will be updated
	// to contain the new state of the ClusterExtension, which contains the status update, but (critically)
	// does not contain the finalizers. After the status update, we need to re-add the finalizers to the
	// reconciledExt before updating the object.
	finalizers := reconciledExt.Finalizers
	if updateStatus {
		if err := r.Client.Status().Update(ctx, reconciledExt); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("error updating status: %v", err))
		}
	}
	reconciledExt.Finalizers = finalizers

	if updateFinalizers {
		if err := r.Client.Update(ctx, reconciledExt); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("error updating finalizers: %v", err))
		}
	}

	return res, reconcileErr
}

// ensureAllConditionsWithReason checks that all defined condition types exist in the given ClusterExtension,
// and assigns a specified reason and custom message to any missing condition.
func ensureAllConditionsWithReason(ext *ocv1alpha1.ClusterExtension, reason v1alpha1.ConditionReason, message string) {
	for _, condType := range conditionsets.ConditionTypes {
		cond := apimeta.FindStatusCondition(ext.Status.Conditions, condType)
		if cond == nil {
			// Create a new condition with a valid reason and add it
			newCond := metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionFalse,
				Reason:             string(reason),
				Message:            message,
				ObservedGeneration: ext.GetGeneration(),
				LastTransitionTime: metav1.NewTime(time.Now()),
			}
			ext.Status.Conditions = append(ext.Status.Conditions, newCond)
		}
	}
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
/* The reconcile functions performs the following major tasks:
1. Resolution: Run the resolution to find the bundle from the catalog which needs to be installed.
2. Validate: Ensure that the bundle returned from the resolution for install meets our requirements.
3. Unpack: Unpack the contents from the bundle and store in a localdir in the pod.
4. Install: The process of installing involves:
4.1 Converting the CSV in the bundle into a set of plain k8s objects.
4.2 Generating a chart from k8s objects.
4.3 Apply the release on cluster.
*/
//nolint:unparam
func (r *ClusterExtensionReconciler) reconcile(ctx context.Context, ext *ocv1alpha1.ClusterExtension) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	l.Info("handling finalizers")
	finalizeResult, err := r.Finalizers.Finalize(ctx, ext)
	if err != nil {
		setStatusProgressing(ext, err)
		return ctrl.Result{}, err
	}
	if finalizeResult.Updated || finalizeResult.StatusUpdated {
		// On create: make sure the finalizer is applied before we do anything
		// On delete: make sure we do nothing after the finalizer is removed
		return ctrl.Result{}, nil
	}

	l.Info("getting installed bundle")
	installedBundle, err := r.InstalledBundleGetter.GetInstalledBundle(ctx, ext)
	if err != nil {
		setInstallStatus(ext, nil)
		// TODO: use Installed=Unknown
		setInstalledStatusConditionFailed(ext, err.Error())
		setStatusProgressing(ext, err)
		return ctrl.Result{}, err
	}

	// run resolution
	l.Info("resolving bundle")
	resolvedBundle, resolvedBundleVersion, resolvedDeprecation, err := r.Resolver.Resolve(ctx, ext, installedBundle)
	if err != nil {
		// Note: We don't distinguish between resolution-specific errors and generic errors
		setInstallStatus(ext, nil)
		setStatusProgressing(ext, err)
		ensureAllConditionsWithReason(ext, ocv1alpha1.ReasonFailed, err.Error())
		return ctrl.Result{}, err
	}

	// set deprecation status after _successful_ resolution
	// TODO:
	//  1. It seems like deprecation status should reflect the currently installed bundle, not the resolved
	//     bundle. So perhaps we should set package and channel deprecations directly after resolution, but
	//     defer setting the bundle deprecation until we successfully install the bundle.
	//  2. If resolution fails because it can't find a bundle, that doesn't mean we wouldn't be able to find
	//     a deprecation for the ClusterExtension's spec.packageName. Perhaps we should check for a non-nil
	//     resolvedDeprecation even if resolution returns an error. If present, we can still update some of
	//     our deprecation status.
	//       - Open question though: what if different catalogs have different opinions of what's deprecated.
	//         If we can't resolve a bundle, how do we know which catalog to trust for deprecation information?
	//         Perhaps if the package shows up in multiple catalogs and deprecations don't match, we can set
	//         the deprecation status to unknown? Or perhaps we somehow combine the deprecation information from
	//         all catalogs?
	SetDeprecationStatus(ext, resolvedBundle.Name, resolvedDeprecation)

	resolvedBundleMetadata := bundleutil.MetadataFor(resolvedBundle.Name, *resolvedBundleVersion)
	bundleSource := &rukpaksource.BundleSource{
		Name: ext.GetName(),
		Type: rukpaksource.SourceTypeImage,
		Image: &rukpaksource.ImageSource{
			Ref: resolvedBundle.Image,
		},
	}
	l.Info("unpacking resolved bundle")
	unpackResult, err := r.Unpacker.Unpack(ctx, bundleSource)
	if err != nil {
		// Wrap the error passed to this with the resolution information until we have successfully
		// installed since we intend for the progressing condition to replace the resolved condition
		// and will be removing the .status.resolution field from the ClusterExtension status API
		setStatusProgressing(ext, wrapErrorWithResolutionInfo(resolvedBundleMetadata, err))
		return ctrl.Result{}, err
	}

	if unpackResult.State != rukpaksource.StateUnpacked {
		panic(fmt.Sprintf("unexpected unpack state %q", unpackResult.State))
	}

	objLbls := map[string]string{
		labels.OwnerKindKey: ocv1alpha1.ClusterExtensionKind,
		labels.OwnerNameKey: ext.GetName(),
	}

	storeLbls := map[string]string{
		labels.BundleNameKey:    resolvedBundle.Name,
		labels.PackageNameKey:   resolvedBundle.Package,
		labels.BundleVersionKey: resolvedBundleVersion.String(),
	}

	l.Info("applying bundle contents")
	// NOTE: We need to be cautious of eating errors here.
	// We should always return any error that occurs during an
	// attempt to apply content to the cluster. Only when there is
	// a verifiable reason to eat the error (i.e it is recoverable)
	// should an exception be made.
	// The following kinds of errors should be returned up the stack
	// to ensure exponential backoff can occur:
	//   - Permission errors (it is not possible to watch changes to permissions.
	//     The only way to eventually recover from permission errors is to keep retrying).
	managedObjs, _, err := r.Applier.Apply(ctx, unpackResult.Bundle, ext, objLbls, storeLbls)
	if err != nil {
		setStatusProgressing(ext, wrapErrorWithResolutionInfo(resolvedBundleMetadata, err))
		// If bundle is not already installed, set Installed status condition to False
		if installedBundle == nil {
			setInstalledStatusConditionFailed(ext, err.Error())
		}
		return ctrl.Result{}, err
	}

	installStatus := &ocv1alpha1.ClusterExtensionInstallStatus{
		Bundle: resolvedBundleMetadata,
	}
	setInstallStatus(ext, installStatus)
	setInstalledStatusConditionSuccess(ext, fmt.Sprintf("Installed bundle %s successfully", resolvedBundle.Image))

	l.Info("watching managed objects")
	cache, err := r.Manager.Get(ctx, ext)
	if err != nil {
		// No need to wrap error with resolution information here (or beyond) since the
		// bundle was successfully installed and the information will be present in
		// the .status.installed field
		setStatusProgressing(ext, err)
		return ctrl.Result{}, err
	}

	if err := cache.Watch(ctx, r.controller, managedObjs...); err != nil {
		setStatusProgressing(ext, err)
		return ctrl.Result{}, err
	}

	// If we made it here, we have successfully reconciled the ClusterExtension
	// and have reached the desired state. Since the Progressing status should reflect
	// our progress towards the desired state, we also set it when we have reached
	// the desired state by providing a nil error value.
	setStatusProgressing(ext, nil)
	return ctrl.Result{}, nil
}

// SetDeprecationStatus will set the appropriate deprecation statuses for a ClusterExtension
// based on the provided bundle
func SetDeprecationStatus(ext *ocv1alpha1.ClusterExtension, bundleName string, deprecation *declcfg.Deprecation) {
	deprecations := map[string][]declcfg.DeprecationEntry{}
	channelSet := sets.New[string]()
	if ext.Spec.Source.Catalog != nil {
		for _, channel := range ext.Spec.Source.Catalog.Channels {
			channelSet.Insert(channel)
		}
	}
	if deprecation != nil {
		for _, entry := range deprecation.Entries {
			switch entry.Reference.Schema {
			case declcfg.SchemaPackage:
				deprecations[ocv1alpha1.TypePackageDeprecated] = []declcfg.DeprecationEntry{entry}
			case declcfg.SchemaChannel:
				if channelSet.Has(entry.Reference.Name) {
					deprecations[ocv1alpha1.TypeChannelDeprecated] = append(deprecations[ocv1alpha1.TypeChannelDeprecated], entry)
				}
			case declcfg.SchemaBundle:
				if bundleName != entry.Reference.Name {
					continue
				}
				deprecations[ocv1alpha1.TypeBundleDeprecated] = []declcfg.DeprecationEntry{entry}
			}
		}
	}

	// first get ordered deprecation messages that we'll join in the Deprecated condition message
	var deprecationMessages []string
	for _, conditionType := range []string{
		ocv1alpha1.TypePackageDeprecated,
		ocv1alpha1.TypeChannelDeprecated,
		ocv1alpha1.TypeBundleDeprecated,
	} {
		if entries, ok := deprecations[conditionType]; ok {
			for _, entry := range entries {
				deprecationMessages = append(deprecationMessages, entry.Message)
			}
		}
	}

	// next, set the Deprecated condition
	status, reason, message := metav1.ConditionFalse, ocv1alpha1.ReasonDeprecated, ""
	if len(deprecationMessages) > 0 {
		status, reason, message = metav1.ConditionTrue, ocv1alpha1.ReasonDeprecated, strings.Join(deprecationMessages, ";")
	}
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeDeprecated,
		Reason:             reason,
		Status:             status,
		Message:            message,
		ObservedGeneration: ext.Generation,
	})

	// finally, set the individual deprecation conditions for package, channel, and bundle
	for _, conditionType := range []string{
		ocv1alpha1.TypePackageDeprecated,
		ocv1alpha1.TypeChannelDeprecated,
		ocv1alpha1.TypeBundleDeprecated,
	} {
		entries, ok := deprecations[conditionType]
		status, reason, message := metav1.ConditionFalse, ocv1alpha1.ReasonDeprecated, ""
		if ok {
			status, reason = metav1.ConditionTrue, ocv1alpha1.ReasonDeprecated
			for _, entry := range entries {
				message = fmt.Sprintf("%s\n%s", message, entry.Message)
			}
		}
		apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               conditionType,
			Reason:             reason,
			Status:             status,
			Message:            message,
			ObservedGeneration: ext.Generation,
		})
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	controller, err := ctrl.NewControllerManagedBy(mgr).
		For(&ocv1alpha1.ClusterExtension{}).
		Watches(&catalogd.ClusterCatalog{},
			crhandler.EnqueueRequestsFromMapFunc(clusterExtensionRequestsForCatalog(mgr.GetClient(), mgr.GetLogger())),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(ue event.UpdateEvent) bool {
					oldObject, isOldCatalog := ue.ObjectOld.(*catalogd.ClusterCatalog)
					newObject, isNewCatalog := ue.ObjectNew.(*catalogd.ClusterCatalog)

					if !isOldCatalog || !isNewCatalog {
						return true
					}

					if oldObject.Status.ResolvedSource != nil && newObject.Status.ResolvedSource != nil {
						if oldObject.Status.ResolvedSource.Image != nil && newObject.Status.ResolvedSource.Image != nil {
							return oldObject.Status.ResolvedSource.Image.ResolvedRef != newObject.Status.ResolvedSource.Image.ResolvedRef
						}
					}
					return true
				},
			})).
		Build(r)
	if err != nil {
		return err
	}
	r.controller = controller
	r.cache = mgr.GetCache()

	return nil
}

func wrapErrorWithResolutionInfo(resolved ocv1alpha1.BundleMetadata, err error) error {
	return fmt.Errorf("%w for resolved bundle %q with version %q", err, resolved.Name, resolved.Version)
}

// Generate reconcile requests for all cluster extensions affected by a catalog change
func clusterExtensionRequestsForCatalog(c client.Reader, logger logr.Logger) crhandler.MapFunc {
	return func(ctx context.Context, _ client.Object) []reconcile.Request {
		// no way of associating an extension to a catalog so create reconcile requests for everything
		clusterExtensions := metav1.PartialObjectMetadataList{}
		clusterExtensions.SetGroupVersionKind(ocv1alpha1.GroupVersion.WithKind("ClusterExtensionList"))
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

type DefaultInstalledBundleGetter struct {
	helmclient.ActionClientGetter
}

func (d *DefaultInstalledBundleGetter) GetInstalledBundle(ctx context.Context, ext *ocv1alpha1.ClusterExtension) (*ocv1alpha1.BundleMetadata, error) {
	cl, err := d.ActionClientFor(ctx, ext)
	if err != nil {
		return nil, err
	}

	release, err := cl.Get(ext.GetName())
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, err
	}
	if release == nil {
		return nil, nil
	}

	return &ocv1alpha1.BundleMetadata{
		Name:    release.Labels[labels.BundleNameKey],
		Version: release.Labels[labels.BundleVersionKey],
	}, nil
}
