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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	crhandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/conditionsets"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	k8sutil "github.com/operator-framework/operator-controller/internal/shared/util/k8s"
)

const (
	ClusterExtensionCleanupUnpackCacheFinalizer         = "olm.operatorframework.io/cleanup-unpack-cache"
	ClusterExtensionCleanupContentManagerCacheFinalizer = "olm.operatorframework.io/cleanup-contentmanager-cache"
)

type reconcileState struct {
	revisionStates           *RevisionStates
	resolvedRevisionMetadata *RevisionMetadata
	imageFS                  fs.FS
}

// ReconcileStepFunc represents a single step in the ClusterExtension reconciliation process.
// It takes a context, state and ClusterExtension object as input and returns:
// - Any error that occurred during reconciliation, which will be returned to the caller
// - A ctrl.Result that indicates whether reconciliation should complete immediately or be retried later
type ReconcileStepFunc func(context.Context, *reconcileState, *ocv1.ClusterExtension) (*ctrl.Result, error)

// ReconcileSteps is an ordered collection of reconciliation steps that are executed sequentially.
// Each step receives the shared state from previous steps, allowing data to flow through the pipeline.
type ReconcileSteps []ReconcileStepFunc

// Reconcile executes a series of reconciliation steps in sequence for a ClusterExtension.
// It takes a context and ClusterExtension object as input and executes each step in the ReconcileSteps slice.
// If any step returns an error, reconciliation stops and the error is returned.
// If any step returns a non-nil ctrl.Result, reconciliation stops, and that result is returned.
// If all steps complete successfully, returns an empty ctrl.Result and nil error.
func (steps *ReconcileSteps) Reconcile(ctx context.Context, ext *ocv1.ClusterExtension) (ctrl.Result, error) {
	var res *ctrl.Result
	var err error
	s := &reconcileState{}
	for _, step := range *steps {
		res, err = step(ctx, s, ext)
		if err != nil {
			return ctrl.Result{}, err
		}
		if res != nil {
			return *res, nil
		}
	}
	return ctrl.Result{}, nil
}

// ClusterExtensionReconciler reconciles a ClusterExtension object
type ClusterExtensionReconciler struct {
	client.Client
	ReconcileSteps ReconcileSteps
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
	res, reconcileErr := r.ReconcileSteps.Reconcile(ctx, reconciledExt)

	// Do checks before any Update()s, as Update() may modify the resource structure!
	updateStatus := !equality.Semantic.DeepEqual(existingExt.Status, reconciledExt.Status)
	updateFinalizers := !equality.Semantic.DeepEqual(existingExt.Finalizers, reconciledExt.Finalizers)

	// If any unexpected fields have changed, panic before updating the resource
	unexpectedFieldsChanged := k8sutil.CheckForUnexpectedFieldChange(existingExt, reconciledExt)
	if unexpectedFieldsChanged {
		panic("spec or metadata changed by reconciler")
	}

	// Save the finalizers off to the side. If we update the status, the reconciledExt will be updated
	// to contain the new state of the ClusterExtension, which contains the status update, but (critically)
	// does not contain the finalizers. After the status update, we will use the saved finalizers in the
	// CreateOrPatch()
	finalizers := reconciledExt.Finalizers
	if updateStatus {
		if err := r.Client.Status().Update(ctx, reconciledExt); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("error updating status: %v", err))
		}
	}

	if updateFinalizers {
		// Use CreateOrPatch to update finalizers on the server
		if _, err := controllerutil.CreateOrPatch(ctx, r.Client, reconciledExt, func() error {
			reconciledExt.Finalizers = finalizers
			return nil
		}); err != nil {
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

// deprecationInfo captures the deprecation data needed to update condition status.
type deprecationInfo struct {
	PackageEntries []declcfg.DeprecationEntry
	ChannelEntries []declcfg.DeprecationEntry
	BundleEntries  []declcfg.DeprecationEntry
	BundleStatus   metav1.ConditionStatus
}

// buildDeprecationInfo filters the catalog deprecation data down to the package, channel,
// and bundle entries that matter for this ClusterExtension. An empty bundle name means
// nothing is installed yet, so we leave bundle status Unknown/Absent.
func buildDeprecationInfo(ext *ocv1.ClusterExtension, installedBundleName string, deprecation *declcfg.Deprecation) deprecationInfo {
	info := deprecationInfo{BundleStatus: metav1.ConditionUnknown}
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
	RevisionName string
	Package      string
	Image        string
	ocv1.BundleMetadata
	Conditions []metav1.Condition
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
