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
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	apimachyaml "k8s.io/apimachinery/pkg/util/yaml"
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
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"
	registryv1handler "github.com/operator-framework/rukpak/pkg/handler"
	crdupgradesafety "github.com/operator-framework/rukpak/pkg/preflights/crdupgradesafety"
	rukpaksource "github.com/operator-framework/rukpak/pkg/source"
	"github.com/operator-framework/rukpak/pkg/storage"
	"github.com/operator-framework/rukpak/pkg/util"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	"github.com/operator-framework/operator-controller/internal/conditionsets"
	"github.com/operator-framework/operator-controller/internal/httputil"
	"github.com/operator-framework/operator-controller/internal/labels"
)

const (
	maxHelmReleaseHistory = 10
)

// ClusterExtensionReconciler reconciles a ClusterExtension object
type ClusterExtensionReconciler struct {
	client.Client
	BundleProvider        BundleProvider
	Unpacker              rukpaksource.Unpacker
	ActionClientGetter    helmclient.ActionClientGetter
	Storage               storage.Storage
	Handler               registryv1handler.Handler
	dynamicWatchMutex     sync.RWMutex
	dynamicWatchGVKs      sets.Set[schema.GroupVersionKind]
	controller            crcontroller.Controller
	cache                 cache.Cache
	InstalledBundleGetter InstalledBundleGetter
	Finalizers            crfinalizer.Finalizers
	CaCertDir             string
	Preflights            []Preflight
}

type InstalledBundleGetter interface {
	GetInstalledBundle(ctx context.Context, ext *ocv1alpha1.ClusterExtension) (*ocv1alpha1.BundleMetadata, error)
}

// Preflight is a check that should be run before making any changes to the cluster
type Preflight interface {
	// Install runs checks that should be successful prior
	// to installing the Helm release. It is provided
	// a Helm release and returns an error if the
	// check is unsuccessful
	Install(context.Context, *release.Release) error

	// Upgrade runs checks that should be successful prior
	// to upgrading the Helm release. It is provided
	// a Helm release and returns an error if the
	// check is unsuccessful
	Upgrade(context.Context, *release.Release) error
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions,verbs=get;list;watch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions/status,verbs=update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=create;update;patch;delete;get;list;watch
//+kubebuilder:rbac:groups=*,resources=*,verbs=*

//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=clustercatalogs,verbs=list;watch
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogmetadata,verbs=list;watch

// The operator controller needs to watch all the bundle objects and reconcile accordingly. Though not ideal, but these permissions are required.
// This has been taken from rukpak, and an issue was created before to discuss it: https://github.com/operator-framework/rukpak/issues/800.
func (r *ClusterExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("operator-controller")
	l.V(1).Info("starting")
	defer l.V(1).Info("ending")

	var existingExt = &ocv1alpha1.ClusterExtension{}
	if err := r.Client.Get(ctx, req.NamespacedName, existingExt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var updateError error

	reconciledExt := existingExt.DeepCopy()
	res, err := r.reconcile(ctx, reconciledExt)
	updateError = errors.Join(updateError, err)

	// Do checks before any Update()s, as Update() may modify the resource structure!
	updateStatus := !equality.Semantic.DeepEqual(existingExt.Status, reconciledExt.Status)
	updateFinalizers := !equality.Semantic.DeepEqual(existingExt.Finalizers, reconciledExt.Finalizers)
	unexpectedFieldsChanged := checkForUnexpectedFieldChange(*existingExt, *reconciledExt)

	if updateStatus {
		err = r.Client.Status().Update(ctx, reconciledExt)
		updateError = errors.Join(updateError, err)
	}

	if unexpectedFieldsChanged {
		panic("spec or metadata changed by reconciler")
	}

	if updateFinalizers {
		err = r.Client.Update(ctx, reconciledExt)
		updateError = errors.Join(updateError, err)
	}

	return res, updateError
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
	finalizeResult, err := r.Finalizers.Finalize(ctx, ext)
	if err != nil {
		// TODO: For now, this error handling follows the pattern of other error handling.
		//  Namely: zero just about everything out, throw our hands up, and return an error.
		//  This is not ideal, and we should consider a more nuanced approach that resolves
		//  as much status as possible before returning, or at least keeps previous state if
		//  it is properly labeled with its observed generation.
		ext.Status.ResolvedBundle = nil
		ext.Status.InstalledBundle = nil
		setResolvedStatusConditionFailed(ext, err.Error())
		ensureAllConditionsWithReason(ext, ocv1alpha1.ReasonResolutionFailed, err.Error())
		return ctrl.Result{}, err
	}
	if finalizeResult.Updated || finalizeResult.StatusUpdated {
		// On create: make sure the finalizer is applied before we do anything
		// On delete: make sure we do nothing after the finalizer is removed
		return ctrl.Result{}, nil
	}

	// run resolution
	bundle, err := r.resolve(ctx, *ext)
	if err != nil {
		// Note: We don't distinguish between resolution-specific errors and generic errors
		ext.Status.ResolvedBundle = nil
		ext.Status.InstalledBundle = nil
		setResolvedStatusConditionFailed(ext, err.Error())
		ensureAllConditionsWithReason(ext, ocv1alpha1.ReasonResolutionFailed, err.Error())
		return ctrl.Result{}, err
	}

	if err := r.validateBundle(bundle); err != nil {
		ext.Status.ResolvedBundle = nil
		ext.Status.InstalledBundle = nil
		setResolvedStatusConditionFailed(ext, err.Error())
		setInstalledStatusConditionFailed(ext, err.Error())
		setDeprecationStatusesUnknown(ext, "deprecation checks have not been attempted as installation has failed")
		return ctrl.Result{}, err
	}
	// set deprecation status after _successful_ resolution
	SetDeprecationStatus(ext, bundle)

	bundleVersion, err := bundle.Version()
	if err != nil {
		ext.Status.ResolvedBundle = nil
		ext.Status.InstalledBundle = nil
		setResolvedStatusConditionFailed(ext, err.Error())
		setInstalledStatusConditionFailed(ext, err.Error())
		return ctrl.Result{}, err
	}

	ext.Status.ResolvedBundle = bundleMetadataFor(bundle)
	setResolvedStatusConditionSuccess(ext, fmt.Sprintf("resolved to %q", bundle.Image))

	// Generate a BundleDeployment from the ClusterExtension to Unpack.
	// Note: The BundleDeployment here is not a k8s API, its a simple Go struct which
	// necessary embedded values.
	bd := r.generateBundleDeploymentForUnpack(ctx, bundle.Image, ext)
	unpackResult, err := r.Unpacker.Unpack(ctx, bd)
	if err != nil {
		setStatusUnpackFailed(ext, err.Error())
		return ctrl.Result{}, err
	}

	switch unpackResult.State {
	case rukpaksource.StatePending:
		setStatusInstallFalseUnpackFailed(ext, unpackResult.Message)
		setInstalledStatusConditionInstalledFalse(ext, "installation has not been attempted as unpack is pending")

		return ctrl.Result{}, nil
	case rukpaksource.StateUnpacked:
		// TODO: https://github.com/operator-framework/rukpak/pull/897 merged, add finalizer to clean the stored bundles
		if err := r.Storage.Store(ctx, ext, unpackResult.Bundle); err != nil {
			setStatusUnpackFailed(ext, err.Error())
			return ctrl.Result{}, err
		}
		setStatusUnpacked(ext, fmt.Sprintf("unpack successful: %v", unpackResult.Message))
	default:
		setStatusUnpackFailed(ext, "unexpected unpack status")
		// We previously exit with a failed status if error is not nil.
		return ctrl.Result{}, fmt.Errorf("unexpected unpack status: %v", unpackResult.Message)
	}

	bundleFS, err := r.Storage.Load(ctx, ext)
	if err != nil {
		setInstalledStatusConditionFailed(ext, err.Error())
		return ctrl.Result{}, err
	}

	chrt, values, err := r.Handler.Handle(ctx, bundleFS, bd)
	if err != nil {
		setInstalledStatusConditionFailed(ext, err.Error())
		return ctrl.Result{}, err
	}

	ac, err := r.ActionClientGetter.ActionClientFor(ctx, ext)
	if err != nil {
		ext.Status.InstalledBundle = nil
		setInstalledStatusConditionFailed(ext, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonErrorGettingClient, err))
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

	rel, desiredRel, state, err := r.getReleaseState(ac, ext, chrt, values, post)
	if err != nil {
		setInstalledStatusConditionFailed(ext, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonErrorGettingReleaseState, err))
		return ctrl.Result{}, err
	}

	for _, preflight := range r.Preflights {
		if ext.Spec.Preflight != nil && ext.Spec.Preflight.CRDUpgradeSafety != nil {
			if _, ok := preflight.(*crdupgradesafety.Preflight); ok && ext.Spec.Preflight.CRDUpgradeSafety.Disabled {
				// Skip this preflight check because it is of type *crdupgradesafety.Preflight and the CRD Upgrade Safety
				// preflight check has been disabled
				continue
			}
		}
		switch state {
		case stateNeedsInstall:
			err := preflight.Install(ctx, desiredRel)
			if err != nil {
				setInstalledStatusConditionFailed(ext, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonInstallationFailed, err))
				return ctrl.Result{}, err
			}
		case stateNeedsUpgrade:
			err := preflight.Upgrade(ctx, desiredRel)
			if err != nil {
				setInstalledStatusConditionFailed(ext, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonInstallationFailed, err))
				return ctrl.Result{}, err
			}
		}
	}

	switch state {
	case stateNeedsInstall:
		rel, err = ac.Install(ext.GetName(), ext.Spec.InstallNamespace, chrt, values, func(install *action.Install) error {
			install.CreateNamespace = false
			install.Labels = map[string]string{labels.BundleNameKey: bundle.Name, labels.PackageNameKey: bundle.Package, labels.BundleVersionKey: bundleVersion.String()}
			return nil
		}, helmclient.AppendInstallPostRenderer(post))
		if err != nil {
			setInstalledStatusConditionFailed(ext, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonInstallationFailed, err))
			return ctrl.Result{}, err
		}
	case stateNeedsUpgrade:
		rel, err = ac.Upgrade(ext.GetName(), ext.Spec.InstallNamespace, chrt, values, func(upgrade *action.Upgrade) error {
			upgrade.MaxHistory = maxHelmReleaseHistory
			upgrade.Labels = map[string]string{labels.BundleNameKey: bundle.Name, labels.PackageNameKey: bundle.Package, labels.BundleVersionKey: bundleVersion.String()}
			return nil
		}, helmclient.AppendUpgradePostRenderer(post))
		if err != nil {
			setInstalledStatusConditionFailed(ext, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonUpgradeFailed, err))
			return ctrl.Result{}, err
		}
	case stateUnchanged:
		if err := ac.Reconcile(rel); err != nil {
			setInstalledStatusConditionFailed(ext, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonResolutionFailed, err))
			return ctrl.Result{}, err
		}
	default:
		return ctrl.Result{}, fmt.Errorf("unexpected release state %q", state)
	}

	relObjects, err := util.ManifestObjects(strings.NewReader(rel.Manifest), fmt.Sprintf("%s-release-manifest", rel.Name))
	if err != nil {
		setInstalledStatusConditionFailed(ext, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonInstallationFailed, err))
		return ctrl.Result{}, err
	}

	for _, obj := range relObjects {
		if err := func() error {
			r.dynamicWatchMutex.Lock()
			defer r.dynamicWatchMutex.Unlock()

			_, isWatched := r.dynamicWatchGVKs[obj.GetObjectKind().GroupVersionKind()]
			if !isWatched {
				if err := r.controller.Watch(
					source.Kind(r.cache,
						obj,
						crhandler.EnqueueRequestForOwner(r.Scheme(), r.RESTMapper(), ext, crhandler.OnlyControllerOwner()),
					),
				); err != nil {
					return err
				}
				r.dynamicWatchGVKs[obj.GetObjectKind().GroupVersionKind()] = sets.Empty{}
			}
			return nil
		}(); err != nil {
			ext.Status.InstalledBundle = nil
			setInstalledStatusConditionFailed(ext, fmt.Sprintf("%s:%v", ocv1alpha1.ReasonInstallationFailed, err))
			return ctrl.Result{}, err
		}
	}
	ext.Status.InstalledBundle = bundleMetadataFor(bundle)
	setInstalledStatusConditionSuccess(ext, fmt.Sprintf("Instantiated bundle %s successfully", ext.GetName()))

	return ctrl.Result{}, nil
}

// resolve returns a Bundle from the catalog that needs to get installed on the cluster.
func (r *ClusterExtensionReconciler) resolve(ctx context.Context, ext ocv1alpha1.ClusterExtension) (*catalogmetadata.Bundle, error) {
	packageName := ext.Spec.PackageName
	channelName := ext.Spec.Channel
	versionRange := ext.Spec.Version

	allBundles, err := r.BundleProvider.Bundles(ctx, packageName)
	if err != nil {
		return nil, fmt.Errorf("error fetching bundles: %w", err)
	}

	installedBundle, err := r.InstalledBundleGetter.GetInstalledBundle(ctx, &ext)
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
			return nil, fmt.Errorf("invalid version range %q: %w", versionRange, err)
		}
		predicates = append(predicates, catalogfilter.InMastermindsSemverRange(vr))
	}

	if ext.Spec.UpgradeConstraintPolicy != ocv1alpha1.UpgradeConstraintPolicyIgnore && installedBundle != nil {
		upgradePredicate, err := SuccessorsPredicate(ext.Spec.PackageName, installedBundle)
		if err != nil {
			return nil, err
		}

		predicates = append(predicates, upgradePredicate)
	}

	resultSet := catalogfilter.Filter(allBundles, catalogfilter.And(predicates...))

	var upgradeErrorPrefix string
	if installedBundle != nil {
		installedBundleVersion, err := mmsemver.NewVersion(installedBundle.Version)
		if err != nil {
			return nil, err
		}
		upgradeErrorPrefix = fmt.Sprintf("error upgrading from currently installed version %q: ", installedBundleVersion.String())
	}
	if len(resultSet) == 0 {
		switch {
		case versionRange != "" && channelName != "":
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

func (r *ClusterExtensionReconciler) generateBundleDeploymentForUnpack(ctx context.Context, bundlePath string, ce *ocv1alpha1.ClusterExtension) *rukpakv1alpha2.BundleDeployment {
	certData, err := httputil.LoadCerts(r.CaCertDir)
	if err != nil {
		log.FromContext(ctx).WithName("operator-controller").WithValues("cluster-extension", ce.GetName()).Error(err, "unable to get TLS certificate")
	}
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
			InstallNamespace: ce.Spec.InstallNamespace,
			Source: rukpakv1alpha2.BundleSource{
				Type: rukpakv1alpha2.SourceTypeImage,
				Image: &rukpakv1alpha2.ImageSource{
					Ref:             bundlePath,
					CertificateData: certData,
				},
			},
		},
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
	r.dynamicWatchGVKs = sets.New[schema.GroupVersionKind]()

	return nil
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

type releaseState string

const (
	stateNeedsInstall releaseState = "NeedsInstall"
	stateNeedsUpgrade releaseState = "NeedsUpgrade"
	stateUnchanged    releaseState = "Unchanged"
	stateError        releaseState = "Error"
)

func (r *ClusterExtensionReconciler) getReleaseState(cl helmclient.ActionInterface, ext *ocv1alpha1.ClusterExtension, chrt *chart.Chart, values chartutil.Values, post *postrenderer) (*release.Release, *release.Release, releaseState, error) {
	currentRelease, err := cl.Get(ext.GetName())
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, nil, stateError, err
	}
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, nil, stateNeedsInstall, nil
	}

	if errors.Is(err, driver.ErrReleaseNotFound) {
		desiredRelease, err := cl.Install(ext.GetName(), ext.Spec.InstallNamespace, chrt, values, func(i *action.Install) error {
			i.DryRun = true
			i.DryRunOption = "server"
			return nil
		}, helmclient.AppendInstallPostRenderer(post))
		if err != nil {
			return nil, nil, stateError, err
		}
		return nil, desiredRelease, stateNeedsInstall, nil
	}
	desiredRelease, err := cl.Upgrade(ext.GetName(), ext.Spec.InstallNamespace, chrt, values, func(upgrade *action.Upgrade) error {
		upgrade.MaxHistory = maxHelmReleaseHistory
		upgrade.DryRun = true
		upgrade.DryRunOption = "server"
		return nil
	}, helmclient.AppendUpgradePostRenderer(post))
	if err != nil {
		return currentRelease, nil, stateError, err
	}
	relState := stateUnchanged
	if desiredRelease.Manifest != currentRelease.Manifest ||
		currentRelease.Info.Status == release.StatusFailed ||
		currentRelease.Info.Status == release.StatusSuperseded {
		relState = stateNeedsUpgrade
	}
	return currentRelease, desiredRelease, relState, nil
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
	unsupportedProps := sets.New[string](
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
