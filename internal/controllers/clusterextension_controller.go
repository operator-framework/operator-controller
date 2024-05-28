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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"
	"github.com/go-logr/logr"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/postrender"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	apimachyaml "k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	crcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	crhandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"
	helmpredicate "github.com/operator-framework/rukpak/pkg/helm-operator-plugins/predicate"
	rukpaksource "github.com/operator-framework/rukpak/pkg/source"
	"github.com/operator-framework/rukpak/pkg/storage"
	"github.com/operator-framework/rukpak/pkg/util"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	"github.com/operator-framework/operator-controller/internal/conditionsets"
	"github.com/operator-framework/operator-controller/internal/handler"
	"github.com/operator-framework/operator-controller/internal/labels"
)

// ClusterExtensionReconciler reconciles a ClusterExtension object
type ClusterExtensionReconciler struct {
	client.Client
	ReleaseNamespace   string
	BundleProvider     BundleProvider
	Unpacker           rukpaksource.Unpacker
	ActionClientGetter helmclient.ActionClientGetter
	Storage            storage.Storage
	Handler            handler.Handler
	dynamicWatchMutex  sync.RWMutex
	dynamicWatchGVKs   map[schema.GroupVersionKind]struct{}
	controller         crcontroller.Controller
	cache              cache.Cache
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions,verbs=get;list;watch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions/status,verbs=update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods,verbs=list;watch;create;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=list;watch
//+kubebuilder:rbac:groups=core,resources=pods/log,verbs=get
//+kubebuilder:rbac:groups=*,resources=*,verbs=*

//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogs,verbs=list;watch
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogmetadata,verbs=list;watch

// The operator controller needs to watch all the bundle objects and reconcile accordingly. Though not ideal, but these permissions are required.
// This has been taken from rukpak, and an issue was created before to discuss it: https://github.com/operator-framework/rukpak/issues/800.
func (r *ClusterExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("operator-controller")
	l.V(1).Info("starting")
	defer l.V(1).Info("ending")

	var existingExt = &ocv1alpha1.ClusterExtension{}
	if err := r.Client.Get(ctx, req.NamespacedName, existingExt); err != nil {
		return ctrl.Result{}, utilerrors.NewAggregate([]error{client.IgnoreNotFound(err), nil})
	}

	reconciledExt := existingExt.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledExt)

	var updateErrors []error

	// Do checks before any Update()s, as Update() may modify the resource structure!
	updateStatus := !equality.Semantic.DeepEqual(existingExt.Status, reconciledExt.Status)
	updateFinalizers := !equality.Semantic.DeepEqual(existingExt.Finalizers, reconciledExt.Finalizers)
	unexpectedFieldsChanged := checkForUnexpectedFieldChange(*existingExt, *reconciledExt)

	if updateStatus {
		if updateErr := r.Client.Status().Update(ctx, reconciledExt); updateErr != nil {
			updateErrors = append(updateErrors, updateErr)
		}
	}

	if unexpectedFieldsChanged {
		panic("spec or metadata changed by reconciler")
	}

	if updateFinalizers {
		if updateErr := r.Client.Update(ctx, reconciledExt); updateErr != nil {
			updateErrors = append(updateErrors, updateErr)
		}
	}

	if reconcileErr != nil {
		updateErrors = append(updateErrors, reconcileErr)
	}

	return res, utilerrors.NewAggregate(updateErrors)
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

func (r *ClusterExtensionReconciler) handleResolutionErrors(ext *ocv1alpha1.ClusterExtension, err error) (ctrl.Result, error) {
	var aggErrs utilerrors.Aggregate
	if errors.As(err, &aggErrs) {
		for _, err := range aggErrs.Errors() {
			errorMessage := err.Error()
			if strings.Contains(errorMessage, "no package") {
				// Handle no package found errors, potentially setting status conditions
				setResolvedStatusConditionFailed(&ext.Status.Conditions, errorMessage, ext.Generation)
				ensureAllConditionsWithReason(ext, "ResolutionFailed", errorMessage)
			} else if strings.Contains(errorMessage, "invalid version range") {
				// Handle invalid version range errors, potentially setting status conditions
				setResolvedStatusConditionFailed(&ext.Status.Conditions, errorMessage, ext.Generation)
				ensureAllConditionsWithReason(ext, "ResolutionFailed", errorMessage)
			} else {
				// General error handling
				setResolvedStatusConditionFailed(&ext.Status.Conditions, errorMessage, ext.Generation)
				ensureAllConditionsWithReason(ext, "InstallationStatusUnknown", "")
			}
		}
	} else {
		// If the error is not an aggregate, handle it as a general error
		errorMessage := err.Error()
		setResolvedStatusConditionFailed(&ext.Status.Conditions, errorMessage, ext.Generation)
		ensureAllConditionsWithReason(ext, "InstallationStatusUnknown", "")
	}
	ext.Status.ResolvedBundle = nil
	return ctrl.Result{}, err
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
	// run resolution
	bundle, err := r.resolve(ctx, *ext)
	if err != nil {
		return r.handleResolutionErrors(ext, err)
	}

	if err := r.validateBundle(bundle); err != nil {
		setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as installation has failed", ext.GetGeneration())
		return ctrl.Result{}, err
	}
	// set deprecation status after _successful_ resolution
	SetDeprecationStatus(ext, bundle)

	bundleVersion, err := bundle.Version()
	if err != nil {
		ext.Status.ResolvedBundle = nil
		ext.Status.InstalledBundle = nil
		setResolvedStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.GetGeneration())
		setInstalledStatusConditionFailed(&ext.Status.Conditions, err.Error(), ext.Generation)
		return ctrl.Result{}, err
	}

	ext.Status.ResolvedBundle = bundleMetadataFor(bundle)
	setResolvedStatusConditionSuccess(&ext.Status.Conditions, fmt.Sprintf("resolved to %q", bundle.Image), ext.GetGeneration())

	// Generate a BundleDeployment from the ClusterExtension to Unpack.
	// Note: The BundleDeployment here is not a k8s API, its a simple Go struct which
	// necessary embedded values.
	bd := r.generateBundleDeploymentForUnpack(bundle.Image, ext)
	unpackResult, err := r.Unpacker.Unpack(ctx, bd)
	if err != nil {
		return ctrl.Result{}, updateStatusUnpackFailing(&ext.Status, err)
	}

	switch unpackResult.State {
	case rukpaksource.StatePending:
		updateStatusUnpackPending(&ext.Status, unpackResult, ext.GetGeneration())
		// There must be a limit to number of entries if status is stuck at
		// unpack pending.
		setHasValidBundleUnknown(&ext.Status.Conditions, "unpack pending", ext.GetGeneration())
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "installation has not been attempted as unpack is pending", ext.GetGeneration())

		return ctrl.Result{}, nil
	case rukpaksource.StateUnpacking:
		updateStatusUnpacking(&ext.Status, unpackResult)
		setHasValidBundleUnknown(&ext.Status.Conditions, "unpack pending", ext.GetGeneration())
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "installation has not been attempted as unpack is pending", ext.GetGeneration())
		return ctrl.Result{}, nil
	case rukpaksource.StateUnpacked:
		// TODO: Add finalizer to clean the stored bundles, after https://github.com/operator-framework/rukpak/pull/897
		// merges.
		if err := r.Storage.Store(ctx, ext, unpackResult.Bundle); err != nil {
			return ctrl.Result{}, updateStatusUnpackFailing(&ext.Status, err)
		}
		updateStatusUnpacked(&ext.Status, unpackResult)
	default:
		return ctrl.Result{}, updateStatusUnpackFailing(&ext.Status, err)
	}

	bundleFS, err := r.Storage.Load(ctx, ext)
	if err != nil {
		apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:    ocv1alpha1.TypeHasValidBundle,
			Status:  metav1.ConditionFalse,
			Reason:  ocv1alpha1.ReasonBundleLoadFailed,
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}

	chrt, values, err := r.Handler.Handle(ctx, bundleFS, ext)
	if err != nil {
		apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:    ocv1alpha1.TypeInstalled,
			Status:  metav1.ConditionFalse,
			Reason:  ocv1alpha1.ReasonInstallationFailed,
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}

	ac, err := r.ActionClientGetter.ActionClientFor(ctx, ext)
	if err != nil {
		ext.Status.InstalledBundle = nil
		setInstalledStatusConditionFailed(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonErrorGettingClient, err), ext.Generation)
		return ctrl.Result{}, err
	}

	post := &postrenderer{
		labels: map[string]string{
			labels.OwnerKindKey:     ocv1alpha1.ClusterExtensionKind,
			labels.OwnerNameKey:     ext.GetName(),
			labels.BundleNameKey:    bundle.Name,
			labels.PackageNameKey:   bundle.Package,
			labels.BundleVersionKey: bundleVersion.String(),
		},
	}

	rel, state, err := r.getReleaseState(ac, ext, chrt, values, post)
	if err != nil {
		setInstalledStatusConditionFailed(&ext.Status.Conditions, fmt.Sprintf("%s:%v", rukpakv1alpha2.ReasonErrorGettingReleaseState, err), ext.Generation)
		return ctrl.Result{}, err
	}

	switch state {
	case stateNeedsInstall:
		rel, err = ac.Install(ext.GetName(), r.ReleaseNamespace, chrt, values, func(install *action.Install) error {
			install.CreateNamespace = false
			install.Labels = map[string]string{labels.BundleNameKey: bundle.Name, labels.PackageNameKey: bundle.Package, labels.BundleVersionKey: bundleVersion.String()}
			return nil
		}, helmclient.AppendInstallPostRenderer(post))
		if err != nil {
			if isResourceNotFoundErr(err) {
				err = errRequiredResourceNotFound{err}
			}
			setInstalledStatusConditionFailed(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonInstallationFailed, err), ext.Generation)
			return ctrl.Result{}, err
		}
	case stateNeedsUpgrade:
		rel, err = ac.Upgrade(ext.GetName(), r.ReleaseNamespace, chrt, values, helmclient.AppendUpgradePostRenderer(post))
		if err != nil {
			if isResourceNotFoundErr(err) {
				err = errRequiredResourceNotFound{err}
			}
			setInstalledStatusConditionFailed(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonUpgradeFailed, err), ext.Generation)
			return ctrl.Result{}, err
		}
	case stateUnchanged:
		if err := ac.Reconcile(rel); err != nil {
			if isResourceNotFoundErr(err) {
				err = errRequiredResourceNotFound{err}
			}
			setInstalledStatusConditionFailed(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonResolutionFailed, err), ext.Generation)
			return ctrl.Result{}, err
		}
	default:
		return ctrl.Result{}, fmt.Errorf("unexpected release state %q", state)
	}

	relObjects, err := util.ManifestObjects(strings.NewReader(rel.Manifest), fmt.Sprintf("%s-release-manifest", rel.Name))
	if err != nil {
		setInstalledStatusConditionFailed(&ext.Status.Conditions, fmt.Sprintf("%s:%v", rukpakv1alpha2.ReasonCreateDynamicWatchFailed, err), ext.Generation)
		return ctrl.Result{}, err
	}

	for _, obj := range relObjects {
		uMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			setInstalledStatusConditionFailed(&ext.Status.Conditions, fmt.Sprintf("%s:%v", rukpakv1alpha2.ReasonCreateDynamicWatchFailed, err), ext.Generation)
			return ctrl.Result{}, err
		}

		unstructuredObj := &unstructured.Unstructured{Object: uMap}
		if err := func() error {
			r.dynamicWatchMutex.Lock()
			defer r.dynamicWatchMutex.Unlock()

			_, isWatched := r.dynamicWatchGVKs[unstructuredObj.GroupVersionKind()]
			if !isWatched {
				if err := r.controller.Watch(
					source.Kind(r.cache, unstructuredObj),
					crhandler.EnqueueRequestForOwner(r.Scheme(), r.RESTMapper(), ext, crhandler.OnlyControllerOwner()),
					helmpredicate.DependentPredicateFuncs()); err != nil {
					return err
				}
				r.dynamicWatchGVKs[unstructuredObj.GroupVersionKind()] = struct{}{}
			}
			return nil
		}(); err != nil {
			ext.Status.InstalledBundle = nil
			setInstalledStatusConditionFailed(&ext.Status.Conditions, fmt.Sprintf("%s:%v", rukpakv1alpha2.ReasonCreateDynamicWatchFailed, err), ext.Generation)
			return ctrl.Result{}, err
		}
	}
	ext.Status.InstalledBundle = bundleMetadataFor(bundle)
	setInstalledStatusConditionSuccess(&ext.Status.Conditions, fmt.Sprintf("Instantiated bundle %s successfully", ext.GetName()), ext.Generation)

	return ctrl.Result{}, nil
}

// resolve returns a Bundle from the catalog that needs to get installed on the cluster.
func (r *ClusterExtensionReconciler) resolve(ctx context.Context, ext ocv1alpha1.ClusterExtension) (*catalogmetadata.Bundle, error) {
	allBundles, err := r.BundleProvider.Bundles(ctx)
	if err != nil {
		return nil, utilerrors.NewAggregate([]error{fmt.Errorf("error fetching bundles: %w", err)})
	}

	packageName := ext.Spec.PackageName
	channelName := ext.Spec.Channel
	versionRange := ext.Spec.Version

	installedBundle, err := GetInstalledbundle(ctx, r.ActionClientGetter, allBundles, &ext)
	if err != nil {
		return nil, err
	}

	predicates := []catalogfilter.Predicate[catalogmetadata.Bundle]{
		catalogfilter.WithPackageName(packageName),
	}

	if channelName != "" {
		predicates = append(predicates, catalogfilter.InChannel(channelName))
	}

	if versionRange != "" {
		vr, err := mmsemver.NewConstraint(versionRange)
		if err != nil {
			return nil, utilerrors.NewAggregate([]error{fmt.Errorf("invalid version range '%s': %w", versionRange, err)})
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
		fmt.Println("upgrade error!!!!")
		upgradeErrorPrefix = fmt.Sprintf("error upgrading from currently installed version %q: ", installedBundleVersion.String())
	}
	if len(resultSet) == 0 {
		fmt.Println("empty resilt set!!")
		switch {
		case versionRange != "" && channelName != "":
			fmt.Println("here!!!")
			return nil, fmt.Errorf("%sno package %q matching version %q in channel %q found", upgradeErrorPrefix, packageName, versionRange, channelName)
		case versionRange != "":
			return nil, fmt.Errorf("%sno package %q matching version %q found", upgradeErrorPrefix, packageName, versionRange)
		case channelName != "":
			return nil, fmt.Errorf("%sno package %q in channel %q found", upgradeErrorPrefix, packageName, channelName)
		default:
			return nil, fmt.Errorf("%sno package %q found", upgradeErrorPrefix, packageName)
		}
	}

	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByVersion(resultSet[i], resultSet[j])
	})
	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByDeprecated(resultSet[i], resultSet[j])
	})

	return resultSet[0], nil
}

// SetDeprecationStatus will set the appropriate deprecation statuses for a ClusterExtension
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

func (r *ClusterExtensionReconciler) generateBundleDeploymentForUnpack(bundlePath string, ce *ocv1alpha1.ClusterExtension) *rukpakv1alpha2.BundleDeployment {
	return &rukpakv1alpha2.BundleDeployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       ce.Kind,
			APIVersion: ce.APIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: ce.Name,
			UID:  ce.UID,
		},
		Spec: rukpakv1alpha2.BundleDeploymentSpec{
			Source: rukpakv1alpha2.BundleSource{
				Type: rukpakv1alpha2.SourceTypeImage,
				Image: &rukpakv1alpha2.ImageSource{
					Ref: bundlePath,
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	controller, err := ctrl.NewControllerManagedBy(mgr).
		For(&ocv1alpha1.ClusterExtension{}).
		Watches(&catalogd.Catalog{},
			crhandler.EnqueueRequestsFromMapFunc(clusterExtensionRequestsForCatalog(mgr.GetClient(), mgr.GetLogger()))).
		WithEventFilter(predicate.Funcs{
			UpdateFunc: func(ue event.UpdateEvent) bool {
				oldObject, isOldCatalog := ue.ObjectOld.(*catalogd.Catalog)
				newObject, isNewCatalog := ue.ObjectNew.(*catalogd.Catalog)

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
		}).
		Watches(&corev1.Pod{}, mapOwneeToOwnerHandler(mgr.GetClient(), mgr.GetLogger(), &ocv1alpha1.ClusterExtension{})).
		Build(r)

	if err != nil {
		return err
	}
	r.controller = controller
	r.cache = mgr.GetCache()
	r.dynamicWatchGVKs = map[schema.GroupVersionKind]struct{}{}
	return nil
}

func mapOwneeToOwnerHandler(cl client.Client, log logr.Logger, owner client.Object) crhandler.EventHandler {
	return crhandler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		ownerGVK, err := apiutil.GVKForObject(owner, cl.Scheme())
		if err != nil {
			log.Error(err, "map ownee to owner: lookup GVK for owner")
			return nil
		}

		type ownerInfo struct {
			key types.NamespacedName
			gvk schema.GroupVersionKind
		}
		var oi *ownerInfo

		for _, ref := range obj.GetOwnerReferences() {
			gv, err := schema.ParseGroupVersion(ref.APIVersion)
			if err != nil {
				log.Error(err, fmt.Sprintf("map ownee to owner: parse ownee's owner reference group version %q", ref.APIVersion))
				return nil
			}
			refGVK := gv.WithKind(ref.Kind)
			if refGVK == ownerGVK && ref.Controller != nil && *ref.Controller {
				oi = &ownerInfo{
					key: types.NamespacedName{Name: ref.Name},
					gvk: ownerGVK,
				}
				break
			}
		}
		if oi == nil {
			return nil
		}
		return []reconcile.Request{{NamespacedName: oi.key}}
	})
}

// Generate reconcile requests for all cluster extensions affected by a catalog change
func clusterExtensionRequestsForCatalog(c client.Reader, logger logr.Logger) crhandler.MapFunc {
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

type releaseState string

const (
	stateNeedsInstall releaseState = "NeedsInstall"
	stateNeedsUpgrade releaseState = "NeedsUpgrade"
	stateUnchanged    releaseState = "Unchanged"
	stateError        releaseState = "Error"
)

func (r *ClusterExtensionReconciler) getReleaseState(cl helmclient.ActionInterface, obj metav1.Object, chrt *chart.Chart, values chartutil.Values, post *postrenderer) (*release.Release, releaseState, error) {
	currentRelease, err := cl.Get(obj.GetName())
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, stateError, err
	}
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, stateNeedsInstall, nil
	}

	desiredRelease, err := cl.Upgrade(obj.GetName(), r.ReleaseNamespace, chrt, values, func(upgrade *action.Upgrade) error {
		upgrade.DryRun = true
		return nil
	}, helmclient.AppendUpgradePostRenderer(post))
	if err != nil {
		return currentRelease, stateError, err
	}
	if desiredRelease.Manifest != currentRelease.Manifest ||
		currentRelease.Info.Status == release.StatusFailed ||
		currentRelease.Info.Status == release.StatusSuperseded {
		return currentRelease, stateNeedsUpgrade, nil
	}
	return currentRelease, stateUnchanged, nil
}

var GetInstalledbundle = func(ctx context.Context, acg helmclient.ActionClientGetter, allBundles []*catalogmetadata.Bundle, ext *ocv1alpha1.ClusterExtension) (*catalogmetadata.Bundle, error) {
	cl, err := acg.ActionClientFor(ctx, ext)
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

	// Bundle must match installed version exactly
	vr, err := mmsemver.NewConstraint(release.Labels[labels.BundleVersionKey])
	if err != nil {
		return nil, err
	}

	// find corresponding bundle for the installed content
	resultSet := catalogfilter.Filter(allBundles, catalogfilter.And(
		catalogfilter.WithPackageName(release.Labels[labels.PackageNameKey]),
		catalogfilter.WithBundleName(release.Labels[labels.BundleNameKey]),
		catalogfilter.InMastermindsSemverRange(vr),
	))
	if len(resultSet) == 0 {
		return nil, fmt.Errorf("bundle %q for package %q not found in available catalogs but is currently installed in namespace %q", release.Labels[labels.BundleNameKey], ext.Spec.PackageName, release.Namespace)
	}

	sort.SliceStable(resultSet, func(i, j int) bool {
		return catalogsort.ByVersion(resultSet[i], resultSet[j])
	})

	return resultSet[0], nil
}

type errRequiredResourceNotFound struct {
	error
}

func (err errRequiredResourceNotFound) Error() string {
	return fmt.Sprintf("required resource not found: %v", err.error)
}

func isResourceNotFoundErr(err error) bool {
	var agg utilerrors.Aggregate
	if errors.As(err, &agg) {
		for _, err := range agg.Errors() {
			return isResourceNotFoundErr(err)
		}
	}

	nkme := &apimeta.NoKindMatchError{}
	if errors.As(err, &nkme) {
		return true
	}
	if apierrors.IsNotFound(err) {
		return true
	}

	// TODO: improve NoKindMatchError matching
	//   An error that is bubbled up from the k8s.io/cli-runtime library
	//   does not wrap meta.NoKindMatchError, so we need to fallback to
	//   the use of string comparisons for now.
	return strings.Contains(err.Error(), "no matches for kind")
}

type postrenderer struct {
	labels  map[string]string
	cascade postrender.PostRenderer
}

func (p *postrenderer) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	dec := apimachyaml.NewYAMLOrJSONDecoder(renderedManifests, 1024)
	for {
		obj := unstructured.Unstructured{}
		err := dec.Decode(&obj)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		obj.SetLabels(util.MergeMaps(obj.GetLabels(), p.labels))
		b, err := obj.MarshalJSON()
		if err != nil {
			return nil, err
		}
		buf.Write(b)
	}
	if p.cascade != nil {
		return p.cascade.Run(&buf)
	}
	return &buf, nil
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
