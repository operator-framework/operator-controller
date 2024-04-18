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
	apimachyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	crcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	crhandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	rukpakapi "github.com/operator-framework/operator-controller/internal/rukpak/api"
	"github.com/operator-framework/operator-controller/internal/rukpak/handler"
	helmpredicate "github.com/operator-framework/operator-controller/internal/rukpak/helm-operator-plugins/predicate"
	rukpaksource "github.com/operator-framework/operator-controller/internal/rukpak/source"
	"github.com/operator-framework/operator-controller/internal/rukpak/storage"
	"github.com/operator-framework/operator-controller/internal/rukpak/util"
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
	Scheme             *runtime.Scheme
	dynamicWatchMutex  sync.RWMutex
	dynamicWatchGVKs   map[schema.GroupVersionKind]struct{}
	controller         crcontroller.Controller
	cache              cache.Cache
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
	// run resolution
	bundle, err := r.resolve(ctx, *ext)
	if err != nil {
		// set right statuses
		return ctrl.Result{}, err
	}

	bundleVersion, err := bundle.Version()
	if err != nil {
		setInstalledStatusConditionFailed(&ext.Status.Conditions, fmt.Sprintf("%s:%v", "unable to get resolved bundle version", err), ext.Generation)
		return ctrl.Result{}, err
	}

	// Now we can set the Resolved Condition, and the resolvedBundleSource field to the bundle.Image value.
	ext.Status.ResolvedBundle = bundleMetadataFor(bundle)
	setResolvedStatusConditionSuccess(&ext.Status.Conditions, fmt.Sprintf("resolved to %q", bundle.Image), ext.GetGeneration())

	// Unpack contents into a fs based on the bundle.
	// Considering only image source.

	// Generate a BundleSource, and then pass this and the ClusterExtension to Unpack
	bs := r.GenerateExpectedBundleSource(bundle.Image)
	unpackResult, err := r.Unpacker.Unpack(ctx, bs, ext)
	if err != nil {
		return ctrl.Result{}, updateStatusUnpackFailing(&ext.Status, fmt.Errorf("source bundle content: %v", err))
	}

	switch unpackResult.State {
	case rukpaksource.StatePending:
		updateStatusUnpackPending(&ext.Status, unpackResult)
		// There must be a limit to number of entries if status is stuck at
		// unpack pending.
		return ctrl.Result{}, nil
	case rukpaksource.StateUnpacking:
		updateStatusUnpacking(&ext.Status, unpackResult)
		return ctrl.Result{}, nil
	case rukpaksource.StateUnpacked:
		if err := r.Storage.Store(ctx, ext, unpackResult.Bundle); err != nil {
			return ctrl.Result{}, updateStatusUnpackFailing(&ext.Status, fmt.Errorf("persist bundle content: %v", err))
		}
		contentURL, err := r.Storage.URLFor(ctx, ext)
		if err != nil {
			return ctrl.Result{}, updateStatusUnpackFailing(&ext.Status, fmt.Errorf("get content URL: %v", err))
		}
		updateStatusUnpacked(&ext.Status, unpackResult, contentURL)
	default:
		return ctrl.Result{}, updateStatusUnpackFailing(&ext.Status, fmt.Errorf("unknown unpack state %q: %v", unpackResult.State, err))
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
			Type:    rukpakv1alpha2.TypeInstalled,
			Status:  metav1.ConditionFalse,
			Reason:  rukpakv1alpha2.ReasonInstallFailed,
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}

	ext.SetNamespace(r.ReleaseNamespace)
	ac, err := r.ActionClientGetter.ActionClientFor(ext)
	if err != nil {
		setInstalledStatusConditionFailed(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonErrorGettingClient, err), ext.Generation)
		return ctrl.Result{}, err
	}

	post := &postrenderer{
		labels: map[string]string{
			util.CoreOwnerKindKey:          ocv1alpha1.ClusterExtensionKind,
			util.CoreOwnerNameKey:          ext.GetName(),
			util.ResolvedbundleName:        bundle.Name,
			util.ResolvedbundlePackageName: bundle.Package,
			util.ResolvedbundleVersion:     bundleVersion.String(),
		},
	}

	rel, state, err := r.getReleaseState(ac, ext, chrt, values, post)
	if err != nil {
		setInstalledAndHealthyFalse(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonErrorGettingReleaseState, err), ext.Generation)
		return ctrl.Result{}, err
	}

	switch state {
	case stateNeedsInstall:
		rel, err = ac.Install(ext.GetName(), r.ReleaseNamespace, chrt, values, func(install *action.Install) error {
			install.CreateNamespace = false
			return nil
		}, helmclient.AppendInstallPostRenderer(post))
		if err != nil {
			if isResourceNotFoundErr(err) {
				err = errRequiredResourceNotFound{err}
			}
			setInstalledAndHealthyFalse(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonInstallationFailed, err), ext.Generation)
			return ctrl.Result{}, err
		}
	case stateNeedsUpgrade:
		rel, err = ac.Upgrade(ext.GetName(), r.ReleaseNamespace, chrt, values, helmclient.AppendUpgradePostRenderer(post))
		if err != nil {
			if isResourceNotFoundErr(err) {
				err = errRequiredResourceNotFound{err}
			}
			setInstalledAndHealthyFalse(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonUpgradeFailed, err), ext.Generation)
			return ctrl.Result{}, err
		}
	case stateUnchanged:
		if err := ac.Reconcile(rel); err != nil {
			if isResourceNotFoundErr(err) {
				err = errRequiredResourceNotFound{err}
			}
			setInstalledAndHealthyFalse(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonResolutionFailed, err), ext.Generation)
			return ctrl.Result{}, err
		}
	default:
		return ctrl.Result{}, fmt.Errorf("unexpected release state %q", state)
	}

	relObjects, err := util.ManifestObjects(strings.NewReader(rel.Manifest), fmt.Sprintf("%s-release-manifest", rel.Name))
	if err != nil {
		setInstalledAndHealthyFalse(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonCreateDynamicWatchFailed, err), ext.Generation)
		return ctrl.Result{}, err
	}

	for _, obj := range relObjects {
		uMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			setInstalledAndHealthyFalse(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonCreateDynamicWatchFailed, err), ext.Generation)
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
					crhandler.EnqueueRequestForOwner(r.Scheme, r.RESTMapper(), ext, crhandler.OnlyControllerOwner()),
					helmpredicate.DependentPredicateFuncs()); err != nil {
					return err
				}
				r.dynamicWatchGVKs[unstructuredObj.GroupVersionKind()] = struct{}{}
			}
			return nil
		}(); err != nil {
			setInstalledAndHealthyFalse(&ext.Status.Conditions, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonCreateDynamicWatchFailed, err), ext.Generation)
			return ctrl.Result{}, err
		}
	}
	setInstalledStatusConditionSuccess(&ext.Status.Conditions, fmt.Sprintf("Instantiated bundle %s successfully", ext.GetName()), ext.Generation)

	// set the status of the cluster extension based on the respective bundle deployment status conditions.
	return ctrl.Result{}, nil
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

func (r *ClusterExtensionReconciler) GenerateExpectedBundleSource(bundlePath string) *rukpakapi.BundleSource {
	return &rukpakapi.BundleSource{
		Type: rukpakapi.SourceTypeImage,
		Image: rukpakapi.ImageSource{
			Ref: bundlePath,
		},
	}
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
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	})
	return bd
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	controller, err := ctrl.NewControllerManagedBy(mgr).
		For(&ocv1alpha1.ClusterExtension{}).
		Watches(&catalogd.Catalog{},
			crhandler.EnqueueRequestsFromMapFunc(clusterExtensionRequestsForCatalog(mgr.GetClient(), mgr.GetLogger()))).
		Watches(&corev1.Pod{}, mapOwneeToOwnerHandler(mgr.GetClient(), mgr.GetLogger(), &ocv1alpha1.ClusterExtension{})).
		Build(r)

	if err != nil {
		return err
	}
	r.controller = controller
	r.cache = mgr.GetCache()
	return nil
}

func mapOwneeToOwnerHandler(cl client.Client, log logr.Logger, owner client.Object) crhandler.EventHandler {
	return crhandler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		ownerGVK, err := apiutil.GVKForObject(owner, cl.Scheme())
		if err != nil {
			log.Error(err, "map ownee to owner: lookup GVK for owner")
			return nil
		}
		owneeGVK, err := apiutil.GVKForObject(obj, cl.Scheme())
		if err != nil {
			log.Error(err, "map ownee to owner: lookup GVK for ownee")
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

		if err := cl.Get(ctx, oi.key, owner); client.IgnoreNotFound(err) != nil {
			log.Info("map ownee to owner: get owner",
				"ownee", client.ObjectKeyFromObject(obj),
				"owneeKind", owneeGVK,
				"owner", oi.key,
				"ownerKind", oi.gvk,
				"error", err.Error(),
			)
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

func (r *ClusterExtensionReconciler) resolve(ctx context.Context, clusterExtension ocv1alpha1.ClusterExtension) (*catalogmetadata.Bundle, error) {
	allBundles, err := r.BundleProvider.Bundles(ctx)
	if err != nil {
		return nil, err
	}

	// TODO: change clusterExtension spec to contain a source field.
	packageName := clusterExtension.Spec.PackageName
	channelName := clusterExtension.Spec.Channel
	versionRange := clusterExtension.Spec.Version

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
	if clusterExtension.Spec.UpgradeConstraintPolicy != ocv1alpha1.UpgradeConstraintPolicyIgnore {
		installedVersionSemver, err := r.getInstalledVersion(clusterExtension)
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

func (r *ClusterExtensionReconciler) getInstalledVersion(clusterExtension ocv1alpha1.ClusterExtension) (*bsemver.Version, error) {
	cl, err := r.ActionClientGetter.ActionClientFor(&clusterExtension)
	if err != nil {
		return nil, err
	}

	// Clarify - Every release will have a unique name as the cluster extension?
	// Also filter relases whose owner is the operator controller?
	// I think this should work, given we are setting the release Name to the clusterExtension name.
	// If not, the other option is to get the Helm secret in the release namespace, list all the releases,
	// get the chart annotations.
	release, err := cl.Get(clusterExtension.GetName())
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, nil
	}

	chart := release.Chart
	if chart == nil {
		return nil, errors.New("empty chart associated with the release")
	}

	// TODO: when the chart is created these annotations are to be added.
	existingVersion, ok := chart.Metadata.Annotations[util.ResolvedbundleVersion]
	if !ok {
		return nil, fmt.Errorf("chart %q: missing bundle version", chart.Name())
	}

	existingVersionSemver, err := bsemver.New(existingVersion)
	if err != nil {
		return nil, fmt.Errorf("could not determine bundle version for the chart %q: %w", chart.Name(), err)
	}
	return existingVersionSemver, nil
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
