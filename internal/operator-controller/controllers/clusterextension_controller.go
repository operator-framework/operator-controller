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
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	crhandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/operator-controller/conditionsets"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	"github.com/operator-framework/operator-controller/internal/operator-controller/resolve"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
)

const (
	ClusterExtensionCleanupUnpackCacheFinalizer         = "olm.operatorframework.io/cleanup-unpack-cache"
	ClusterExtensionCleanupContentManagerCacheFinalizer = "olm.operatorframework.io/cleanup-contentmanager-cache"
)

// ClusterExtensionReconciler reconciles a ClusterExtension object
type ClusterExtensionReconciler struct {
	client.Client
	Resolver resolve.Resolver

	ImageCache  imageutil.Cache
	ImagePuller imageutil.Puller

	StorageMigrator      StorageMigrator
	Applier              Applier
	RevisionStatesGetter RevisionStatesGetter
	Finalizers           crfinalizer.Finalizers
}

type StorageMigrator interface {
	Migrate(context.Context, *ocv1.ClusterExtension, map[string]string) error
}

type Applier interface {
	// Apply applies the content in the provided fs.FS using the configuration of the provided ClusterExtension.
	// It also takes in a map[string]string to be applied to all applied resources as labels and another
	// map[string]string used to create a unique identifier for a stored reference to the resources created.
	Apply(context.Context, fs.FS, *ocv1.ClusterExtension, map[string]string, map[string]string) (bool, string, error)
}

type RevisionStatesGetter interface {
	GetRevisionStates(ctx context.Context, ext *ocv1.ClusterExtension) (*RevisionStates, error)
}

// The operator controller needs to watch all the bundle objects and reconcile accordingly. Though not ideal, but these permissions are required.
// This has been taken from rukpak, and an issue was created before to discuss it: https://github.com/operator-framework/rukpak/issues/800.
func (r *ClusterExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("cluster-extension")
	ctx = log.IntoContext(ctx, l)

	existingExt := &ocv1.ClusterExtension{}
	if err := r.Get(ctx, req.NamespacedName, existingExt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	l.Info("reconcile starting")
	defer l.Info("reconcile ending")

	reconciledExt := existingExt.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledExt)

	// Do checks before any Update()s, as Update() may modify the resource structure!
	updateStatus := !equality.Semantic.DeepEqual(existingExt.Status, reconciledExt.Status)
	updateFinalizers := !equality.Semantic.DeepEqual(existingExt.Finalizers, reconciledExt.Finalizers)

	// If any unexpected fields have changed, panic before updating the resource
	unexpectedFieldsChanged := checkForUnexpectedClusterExtensionFieldChange(*existingExt, *reconciledExt)
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
		if err := r.Update(ctx, reconciledExt); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("error updating finalizers: %v", err))
		}
	}

	return res, reconcileErr
}

// ensureFailureConditionsWithReason keeps every non-deprecation condition present.
// If one is missing, we add it with the given reason and message so users see why
// reconcile failed. Deprecation conditions are handled later by SetDeprecationStatus.
func ensureFailureConditionsWithReason(ext *ocv1.ClusterExtension, reason v1alpha1.ConditionReason, message string) {
	for _, condType := range conditionsets.ConditionTypes {
		if isDeprecationCondition(condType) {
			continue
		}
		cond := apimeta.FindStatusCondition(ext.Status.Conditions, condType)
		// Guard so we only fill empty slots. Without it, we would overwrite the detailed status that
		// helpers (setStatusProgressing, setInstalledStatusCondition*, SetDeprecationStatus) already set.
		if cond == nil {
			// No condition exists yet, so add a fallback with the failure reason. Specific helpers replace it
			// with the real progressing/bundle/package/channel message during reconciliation.
			SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionFalse,
				Reason:             string(reason),
				Message:            message,
				ObservedGeneration: ext.GetGeneration(),
			})
		}
	}
}

// isDeprecationCondition reports whether the given type is one of the deprecation
// conditions we manage separately.
func isDeprecationCondition(condType string) bool {
	switch condType {
	case ocv1.TypeDeprecated, ocv1.TypePackageDeprecated, ocv1.TypeChannelDeprecated, ocv1.TypeBundleDeprecated:
		return true
	default:
		return false
	}
}

// Compare resources - ignoring status & metadata.finalizers
func checkForUnexpectedClusterExtensionFieldChange(a, b ocv1.ClusterExtension) bool {
	a.Status, b.Status = ocv1.ClusterExtensionStatus{}, ocv1.ClusterExtensionStatus{}
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
4.3 Store the release on cluster.
*/
//nolint:unparam
func (r *ClusterExtensionReconciler) reconcile(ctx context.Context, ext *ocv1.ClusterExtension) (ctrl.Result, error) {
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

	if ext.GetDeletionTimestamp() != nil {
		// If we've gotten here, that means the cluster extension is being deleted, we've handled all of
		// _our_ finalizers (above), but the cluster extension is still present in the cluster, likely
		// because there are _other_ finalizers that other controllers need to handle, (e.g. the orphan
		// deletion finalizer).
		return ctrl.Result{}, nil
	}

	objLbls := map[string]string{
		labels.OwnerKindKey: ocv1.ClusterExtensionKind,
		labels.OwnerNameKey: ext.GetName(),
	}

	if r.StorageMigrator != nil {
		if err := r.StorageMigrator.Migrate(ctx, ext, objLbls); err != nil {
			return ctrl.Result{}, fmt.Errorf("migrating storage: %w", err)
		}
	}

	l.Info("getting installed bundle")
	revisionStates, err := r.RevisionStatesGetter.GetRevisionStates(ctx, ext)
	if err != nil {
		setInstallStatus(ext, nil)
		var saerr *authentication.ServiceAccountNotFoundError
		if errors.As(err, &saerr) {
			setInstalledStatusConditionUnknown(ext, saerr.Error())
			setStatusProgressing(ext, errors.New("installation cannot proceed due to missing ServiceAccount"))
			return ctrl.Result{}, err
		}
		setInstalledStatusConditionUnknown(ext, err.Error())
		setStatusProgressing(ext, errors.New("retrying to get installed bundle"))
		return ctrl.Result{}, err
	}

	// Track deprecation data from catalog resolution. We'll update deprecation status
	// explicitly at key points in the reconciliation flow:
	//   1. After resolution (whether it succeeds or fails) - using currently installed bundle
	//   2. After successful rollout - using the newly installed bundle
	//   3. When taking the rolling out path - using currently installed bundle with no catalog data
	var resolvedDeprecation *declcfg.Deprecation
	var hadCatalogDeprecationData bool
	var resolvedRevisionMetadata *RevisionMetadata

	if len(revisionStates.RollingOut) == 0 {
		// Resolve bundle and update deprecation status
		resolvedRevisionMetadata, resolvedDeprecation, hadCatalogDeprecationData, err = r.resolveAndSetDeprecation(ctx, ext, revisionStates)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// Rolling out existing revision - no new catalog data available.
		// Set deprecation to Unknown since we can't query the catalog during rollout.
		installedBundleName := ""
		if revisionStates.Installed != nil {
			installedBundleName = revisionStates.Installed.Name
		}
		SetDeprecationStatus(ext, installedBundleName, nil, false)
		resolvedRevisionMetadata = revisionStates.RollingOut[0]
	}

	l.Info("unpacking resolved bundle")
	imageFS, _, _, err := r.ImagePuller.Pull(ctx, ext.GetName(), resolvedRevisionMetadata.Image, r.ImageCache)
	if err != nil {
		// Wrap the error passed to this with the resolution information until we have successfully
		// installed since we intend for the progressing condition to replace the resolved condition
		// and will be removing the .status.resolution field from the ClusterExtension status API
		setStatusProgressing(ext, wrapErrorWithResolutionInfo(resolvedRevisionMetadata.BundleMetadata, err))
		setInstalledStatusFromRevisionStates(ext, revisionStates)
		return ctrl.Result{}, err
	}

	// The following values will be stored as annotations and not labels
	revisionAnnotations := map[string]string{
		labels.BundleNameKey:      resolvedRevisionMetadata.Name,
		labels.PackageNameKey:     resolvedRevisionMetadata.Package,
		labels.BundleVersionKey:   resolvedRevisionMetadata.Version,
		labels.BundleReferenceKey: resolvedRevisionMetadata.Image,
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
	rolloutSucceeded, rolloutStatus, err := r.Applier.Apply(ctx, imageFS, ext, objLbls, revisionAnnotations)

	// Set installed status
	if rolloutSucceeded {
		revisionStates = &RevisionStates{Installed: resolvedRevisionMetadata}
		// Bundle successfully installed - update deprecation status to reflect the new installed bundle.
		// This ensures BundleDeprecated shows the status of the bundle that actually landed,
		// not the bundle that was previously running or is being resolved.
		SetDeprecationStatus(ext, resolvedRevisionMetadata.Name, resolvedDeprecation, hadCatalogDeprecationData)
	} else if err == nil && revisionStates.Installed == nil && len(revisionStates.RollingOut) == 0 {
		revisionStates = &RevisionStates{RollingOut: []*RevisionMetadata{resolvedRevisionMetadata}}
	}
	setInstalledStatusFromRevisionStates(ext, revisionStates)

	// If there was an error applying the resolved bundle,
	// report the error via the Progressing condition.
	if err != nil {
		setStatusProgressing(ext, wrapErrorWithResolutionInfo(resolvedRevisionMetadata.BundleMetadata, err))
		return ctrl.Result{}, err
	} else if !rolloutSucceeded {
		apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeProgressing,
			Status:             metav1.ConditionTrue,
			Reason:             ocv1.ReasonRolloutInProgress,
			Message:            rolloutStatus,
			ObservedGeneration: ext.GetGeneration(),
		})
	} else {
		setStatusProgressing(ext, nil)
	}
	return ctrl.Result{}, nil
}

// resolveAndSetDeprecation resolves the bundle from the catalog and updates deprecation status.
// Returns the resolved revision metadata, deprecation data, catalog availability flag, and any error.
func (r *ClusterExtensionReconciler) resolveAndSetDeprecation(
	ctx context.Context,
	ext *ocv1.ClusterExtension,
	revisionStates *RevisionStates,
) (*RevisionMetadata, *declcfg.Deprecation, bool, error) {
	var bm *ocv1.BundleMetadata
	if revisionStates.Installed != nil {
		bm = &revisionStates.Installed.BundleMetadata
	}

	resolvedBundle, resolvedBundleVersion, resolvedDeprecation, err := r.Resolver.Resolve(ctx, ext, bm)
	hadCatalogDeprecationData := err == nil || resolvedDeprecation != nil

	// Update deprecation status immediately after resolution.
	// Use the INSTALLED bundle (not the resolved one) so BundleDeprecated reflects
	// what's actually running. Package/Channel deprecations come from catalog data
	// regardless of which bundle is installed.
	installedBundleName := ""
	if revisionStates.Installed != nil {
		installedBundleName = revisionStates.Installed.Name
	}
	SetDeprecationStatus(ext, installedBundleName, resolvedDeprecation, hadCatalogDeprecationData)

	if err != nil {
		// Note: We don't distinguish between resolution-specific errors and generic errors
		setStatusProgressing(ext, err)
		setInstalledStatusFromRevisionStates(ext, revisionStates)
		// Ensure non-deprecation conditions capture the failure immediately.
		// Deprecation status was already set above using any catalog data from the resolver.
		ensureFailureConditionsWithReason(ext, ocv1.ReasonFailed, err.Error())
		return nil, resolvedDeprecation, hadCatalogDeprecationData, err
	}

	resolvedRevisionMetadata := &RevisionMetadata{
		Package:        resolvedBundle.Package,
		Image:          resolvedBundle.Image,
		BundleMetadata: bundleutil.MetadataFor(resolvedBundle.Name, *resolvedBundleVersion),
	}

	return resolvedRevisionMetadata, resolvedDeprecation, hadCatalogDeprecationData, nil
}

// DeprecationInfo captures the deprecation data needed to update condition status.
type DeprecationInfo struct {
	PackageEntries []declcfg.DeprecationEntry
	ChannelEntries []declcfg.DeprecationEntry
	BundleEntries  []declcfg.DeprecationEntry
	BundleStatus   metav1.ConditionStatus
}

// SetDeprecationStatus updates the ClusterExtension deprecation conditions using the
// catalog data from resolve plus the name of the bundle that actually landed. Examples:
//   - no bundle installed -> bundle status stays Unknown/Absent
//   - installed bundle marked deprecated -> bundle status True/Deprecated
//   - installed bundle not deprecated -> bundle status False/Deprecated
//
// This keeps the deprecation conditions focused on catalog information:
//   - PackageDeprecated: true if the catalog marks the package deprecated
//   - ChannelDeprecated: true if any requested channel is marked deprecated
//   - BundleDeprecated: reflects the installed bundle (Unknown/Absent when nothing installed)
//   - Deprecated (rollup): true if any of the above signals a deprecation
//
// Install or validation errors never appear here because they belong on the
// Progressing/Installed conditions instead. Callers should invoke this after reconcile
// finishes (for example via a defer) so catalog data replaces any transient error messages.
//
// TODO: Open question - what if different catalogs have different opinions of what's deprecated?
//
//	If we can't resolve a bundle, how do we know which catalog to trust for deprecation information?
//	Perhaps if the package shows up in multiple catalogs and deprecations don't match, we can set
//	the deprecation status to unknown? Or perhaps we somehow combine the deprecation information from
//	all catalogs?
//
// How it works currently:
//
//	The resolver walks catalogs and picks ONE bundle + ONE deprecation object:
//	  1. For each catalog: filters bundles, sorts (non-deprecated first, then highest version), picks best
//	  2. Compares with prior catalogs: skips deprecated if have non-deprecated, replaces non-deprecated if was deprecated
//	  3. After all catalogs: sorts candidates by priority, picks highest (or fails if tie)
//	  4. Returns winning bundle + that catalog's deprecation object
//
// Scenarios:
//
//	Scenario 1: Same priority, conflicting deprecation
//	  Catalog A (pri 0): "foo" v1.0.0, package deprecated
//	  Catalog B (pri 0): "foo" v1.0.0, package NOT deprecated
//	  - Resolver: A adds v1.0.0 (deprecated), B replaces with v1.0.0 (not deprecated), returns B's deprecation
//	  - PackageDeprecated = False, ChannelDeprecated = False, BundleDeprecated = False
//
//	Scenario 2: Different priority, both bundles not deprecated (priority decides)
//	  Catalog A (pri 1, higher): "foo" v1.0.0, package deprecated but bundle v1.0.0 not deprecated
//	  Catalog B (pri 0, lower): "foo" v1.0.0, package NOT deprecated
//	  - Resolver: A adds v1.0.0 (bundle not deprecated), B adds v1.0.0 (bundle not deprecated)
//	  - Both bundles same deprecation status - both stay in list - priority sort - A wins due priority
//	  - PackageDeprecated = True (A's package is deprecated, even though B says not deprecated)
//
//	Scenario 3: Ambiguity error (resolution fails but has deprecation data)
//	  Catalog A (pri 0): "foo" v1.0.0, not deprecated
//	  Catalog B (pri 0): "foo" v1.0.1, not deprecated
//	  - Resolver: both added (same deprecation status), priority tie - fails, priorDeprecation = B's (last examined)
//	  - Progressing = True (Retrying), PackageDeprecated = False, BundleDeprecated = Unknown/Absent (nothing installed)
//	  - Note: Deprecation data still available even though resolution failed
//
//	Scenario 4: No bundles match version, conflicting deprecations (UNRESOLVED TODO)
//	  Catalog A: "foo" exists (deprecated), no bundles match version ">=2.0.0"
//	  Catalog B: "foo" exists (NOT deprecated), no bundles match version ">=2.0.0"
//	  - Resolver: no bundles pass filter, priorDeprecation = last catalog that had the package (arbitrary)
//	  - Progressing = True (Retrying), PackageDeprecated = ??? (depends which catalog examined last)
//	  - Problem: Using arbitrary catalog's deprecation when catalogs disagree
//	  - TODO: Should we mark Unknown? Combine all? Pick by priority?
func SetDeprecationStatus(ext *ocv1.ClusterExtension, installedBundleName string, deprecation *declcfg.Deprecation, hasCatalogData bool) {
	info := buildDeprecationInfo(ext, installedBundleName, deprecation)
	packageMessages := collectDeprecationMessages(info.PackageEntries)
	channelMessages := collectDeprecationMessages(info.ChannelEntries)
	bundleMessages := collectDeprecationMessages(info.BundleEntries)

	if !hasCatalogData {
		// When catalog is unavailable (e.g. removed), all conditions go Unknown.
		// BundleDeprecated uses Absent only when no bundle installed.
		bundleReason := ocv1.ReasonAbsent
		if installedBundleName != "" {
			bundleReason = ocv1.ReasonDeprecated
		}
		setDeprecationCondition(ext, ocv1.TypeDeprecated, metav1.ConditionUnknown, ocv1.ReasonDeprecated, "")
		setDeprecationCondition(ext, ocv1.TypePackageDeprecated, metav1.ConditionUnknown, ocv1.ReasonDeprecated, "")
		setDeprecationCondition(ext, ocv1.TypeChannelDeprecated, metav1.ConditionUnknown, ocv1.ReasonDeprecated, "")
		setDeprecationCondition(ext, ocv1.TypeBundleDeprecated, metav1.ConditionUnknown, bundleReason, "")
		return
	}

	messages := slices.Concat(packageMessages, channelMessages, bundleMessages)
	deprecatedStatus := metav1.ConditionFalse
	if len(messages) > 0 {
		deprecatedStatus = metav1.ConditionTrue
	}

	setDeprecationCondition(ext, ocv1.TypeDeprecated, deprecatedStatus, ocv1.ReasonDeprecated, strings.Join(messages, "\n"))
	setDeprecationCondition(ext, ocv1.TypePackageDeprecated, conditionStatus(len(packageMessages) > 0), ocv1.ReasonDeprecated, strings.Join(packageMessages, "\n"))
	setDeprecationCondition(ext, ocv1.TypeChannelDeprecated, conditionStatus(len(channelMessages) > 0), ocv1.ReasonDeprecated, strings.Join(channelMessages, "\n"))

	bundleReason := ocv1.ReasonDeprecated
	bundleMessage := strings.Join(bundleMessages, "\n")
	if info.BundleStatus == metav1.ConditionUnknown {
		bundleReason = ocv1.ReasonAbsent
		bundleMessage = ""
	}
	setDeprecationCondition(ext, ocv1.TypeBundleDeprecated, info.BundleStatus, bundleReason, bundleMessage)
}

// setDeprecationCondition sets a single deprecation condition with less boilerplate.
func setDeprecationCondition(ext *ocv1.ClusterExtension, condType string, status metav1.ConditionStatus, reason string, message string) {
	SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// buildDeprecationInfo filters the catalog deprecation data down to the package, channel,
// and bundle entries that matter for this ClusterExtension. An empty bundle name means
// nothing is installed yet, so we leave bundle status Unknown/Absent.
func buildDeprecationInfo(ext *ocv1.ClusterExtension, installedBundleName string, deprecation *declcfg.Deprecation) DeprecationInfo {
	info := DeprecationInfo{BundleStatus: metav1.ConditionUnknown}
	channelSet := sets.New[string]()
	if ext.Spec.Source.Catalog != nil {
		channelSet.Insert(ext.Spec.Source.Catalog.Channels...)
	}

	if deprecation != nil {
		for _, entry := range deprecation.Entries {
			switch entry.Reference.Schema {
			case declcfg.SchemaPackage:
				info.PackageEntries = append(info.PackageEntries, entry)
			case declcfg.SchemaChannel:
				if channelSet.Has(entry.Reference.Name) {
					info.ChannelEntries = append(info.ChannelEntries, entry)
				}
			case declcfg.SchemaBundle:
				if installedBundleName != "" && entry.Reference.Name == installedBundleName {
					info.BundleEntries = append(info.BundleEntries, entry)
				}
			}
		}
	}

	// installedBundleName is empty when nothing is installed. In that case we want
	// to report the bundle deprecation condition as Unknown/Absent.
	if installedBundleName != "" {
		if len(info.BundleEntries) > 0 {
			info.BundleStatus = metav1.ConditionTrue
		} else {
			info.BundleStatus = metav1.ConditionFalse
		}
	}

	return info
}

// collectDeprecationMessages collects the non-empty deprecation messages from the provided entries.
func collectDeprecationMessages(entries []declcfg.DeprecationEntry) []string {
	messages := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Message != "" {
			messages = append(messages, entry.Message)
		}
	}
	return messages
}

func conditionStatus(ok bool) metav1.ConditionStatus {
	if ok {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

type ControllerBuilderOption func(builder *ctrl.Builder)

func WithOwns(obj client.Object) ControllerBuilderOption {
	return func(builder *ctrl.Builder) {
		builder.Owns(obj)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterExtensionReconciler) SetupWithManager(mgr ctrl.Manager, opts ...ControllerBuilderOption) (crcontroller.Controller, error) {
	ctrlBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&ocv1.ClusterExtension{}).
		Named("controller-operator-cluster-extension-controller").
		Watches(&ocv1.ClusterCatalog{},
			crhandler.EnqueueRequestsFromMapFunc(clusterExtensionRequestsForCatalog(mgr.GetClient(), mgr.GetLogger())),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(ue event.UpdateEvent) bool {
					oldObject, isOldCatalog := ue.ObjectOld.(*ocv1.ClusterCatalog)
					newObject, isNewCatalog := ue.ObjectNew.(*ocv1.ClusterCatalog)

					if !isOldCatalog || !isNewCatalog {
						return true
					}

					if oldObject.Status.ResolvedSource != nil && newObject.Status.ResolvedSource != nil {
						if oldObject.Status.ResolvedSource.Image != nil && newObject.Status.ResolvedSource.Image != nil {
							return oldObject.Status.ResolvedSource.Image.Ref != newObject.Status.ResolvedSource.Image.Ref
						}
					}
					return true
				},
			}))

	for _, applyOpt := range opts {
		applyOpt(ctrlBuilder)
	}

	return ctrlBuilder.Build(r)
}

func wrapErrorWithResolutionInfo(resolved ocv1.BundleMetadata, err error) error {
	return fmt.Errorf("error for resolved bundle %q with version %q: %w", resolved.Name, resolved.Version, err)
}

// Generate reconcile requests for all cluster extensions affected by a catalog change
func clusterExtensionRequestsForCatalog(c client.Reader, logger logr.Logger) crhandler.MapFunc {
	return func(ctx context.Context, _ client.Object) []reconcile.Request {
		// no way of associating an extension to a catalog so create reconcile requests for everything
		clusterExtensions := metav1.PartialObjectMetadataList{}
		clusterExtensions.SetGroupVersionKind(ocv1.GroupVersion.WithKind("ClusterExtensionList"))
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

type RevisionMetadata struct {
	Package string
	Image   string
	ocv1.BundleMetadata
}

type RevisionStates struct {
	Installed  *RevisionMetadata
	RollingOut []*RevisionMetadata
}

type HelmRevisionStatesGetter struct {
	helmclient.ActionClientGetter
}

func (d *HelmRevisionStatesGetter) GetRevisionStates(ctx context.Context, ext *ocv1.ClusterExtension) (*RevisionStates, error) {
	cl, err := d.ActionClientFor(ctx, ext)
	if err != nil {
		return nil, err
	}

	relhis, err := cl.History(ext.GetName())
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, err
	}
	rs := &RevisionStates{}
	if len(relhis) == 0 {
		return rs, nil
	}

	// relhis[0].Info.Status is the status of the most recent install attempt.
	// But we need to look for the most-recent _Deployed_ release
	for _, rel := range relhis {
		if rel.Info != nil && rel.Info.Status == release.StatusDeployed {
			rs.Installed = &RevisionMetadata{
				Package: rel.Labels[labels.PackageNameKey],
				Image:   rel.Labels[labels.BundleReferenceKey],
				BundleMetadata: ocv1.BundleMetadata{
					Name:    rel.Labels[labels.BundleNameKey],
					Version: rel.Labels[labels.BundleVersionKey],
				},
			}
			break
		}
	}
	return rs, nil
}

type BoxcutterRevisionStatesGetter struct {
	Reader client.Reader
}

func (d *BoxcutterRevisionStatesGetter) GetRevisionStates(ctx context.Context, ext *ocv1.ClusterExtension) (*RevisionStates, error) {
	// TODO: boxcutter applier has a nearly identical bit of code for listing and sorting revisions
	//   only difference here is that it sorts in reverse order to start iterating with the most
	//   recent revisions. We should consolidate to avoid code duplication.
	existingRevisionList := &ocv1.ClusterExtensionRevisionList{}
	if err := d.Reader.List(ctx, existingRevisionList, client.MatchingLabels{
		labels.OwnerNameKey: ext.Name,
	}); err != nil {
		return nil, fmt.Errorf("listing revisions: %w", err)
	}
	slices.SortFunc(existingRevisionList.Items, func(a, b ocv1.ClusterExtensionRevision) int {
		return cmp.Compare(a.Spec.Revision, b.Spec.Revision)
	})

	rs := &RevisionStates{}
	for _, rev := range existingRevisionList.Items {
		switch rev.Spec.LifecycleState {
		case ocv1.ClusterExtensionRevisionLifecycleStateActive,
			ocv1.ClusterExtensionRevisionLifecycleStatePaused:
		default:
			// Skip anything not active or paused, which should only be "Archived".
			continue
		}

		// TODO: the setting of these annotations (happens in boxcutter applier when we pass in "revisionAnnotations")
		//   is fairly decoupled from this code where we get the annotations back out. We may want to co-locate
		//   the set/get logic a bit better to make it more maintainable and less likely to get out of sync.
		rm := &RevisionMetadata{
			Package: rev.Annotations[labels.PackageNameKey],
			Image:   rev.Annotations[labels.BundleReferenceKey],
			BundleMetadata: ocv1.BundleMetadata{
				Name:    rev.Annotations[labels.BundleNameKey],
				Version: rev.Annotations[labels.BundleVersionKey],
			},
		}

		if apimeta.IsStatusConditionTrue(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded) {
			rs.Installed = rm
		} else {
			rs.RollingOut = append(rs.RollingOut, rm)
		}
	}

	return rs, nil
}
