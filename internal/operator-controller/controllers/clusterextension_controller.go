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
//
//nolint:unparam // reason parameter is designed to be flexible, even if current callers use the same value
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

// SetDeprecationStatus updates deprecation conditions based on catalog metadata.
//
// Behavior (following Kubernetes API conventions - conditions always present):
//   - IS deprecated -> condition True with Reason: Deprecated
//   - NOT deprecated -> condition False with Reason: NotDeprecated
//   - Can't check (no catalog) -> condition Unknown with Reason: DeprecationStatusUnknown
//   - No bundle installed -> BundleDeprecated Unknown with Reason: Absent
//
// This keeps deprecation conditions focused on catalog data. Install/validation errors
// never appear here - they belong in Progressing/Installed conditions.
func SetDeprecationStatus(ext *ocv1.ClusterExtension, installedBundleName string, deprecation *declcfg.Deprecation, hasCatalogData bool) {
	info := buildDeprecationInfo(ext, installedBundleName, deprecation)
	packageMessages := collectDeprecationMessages(info.PackageEntries)
	channelMessages := collectDeprecationMessages(info.ChannelEntries)
	bundleMessages := collectDeprecationMessages(info.BundleEntries)

	// Strategy: Always set deprecation conditions (following Kubernetes API conventions).
	// SetStatusCondition preserves lastTransitionTime when status/reason/message haven't changed,
	// preventing infinite reconciliation loops.
	// - True = deprecated
	// - False = not deprecated (verified via catalog)
	// - Unknown = cannot verify (no catalog data or no bundle installed)

	if !hasCatalogData {
		// When catalog is unavailable, set all to Unknown.
		// BundleDeprecated uses Absent only when no bundle installed.
		bundleReason := ocv1.ReasonAbsent
		bundleMessage := "no bundle installed yet"
		if installedBundleName != "" {
			bundleReason = ocv1.ReasonDeprecationStatusUnknown
			bundleMessage = "deprecation status unknown: catalog data unavailable"
		}
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeDeprecated,
			Status:             metav1.ConditionUnknown,
			Reason:             ocv1.ReasonDeprecationStatusUnknown,
			Message:            "deprecation status unknown: catalog data unavailable",
			ObservedGeneration: ext.GetGeneration(),
		})
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypePackageDeprecated,
			Status:             metav1.ConditionUnknown,
			Reason:             ocv1.ReasonDeprecationStatusUnknown,
			Message:            "deprecation status unknown: catalog data unavailable",
			ObservedGeneration: ext.GetGeneration(),
		})
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeChannelDeprecated,
			Status:             metav1.ConditionUnknown,
			Reason:             ocv1.ReasonDeprecationStatusUnknown,
			Message:            "deprecation status unknown: catalog data unavailable",
			ObservedGeneration: ext.GetGeneration(),
		})
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeBundleDeprecated,
			Status:             metav1.ConditionUnknown,
			Reason:             bundleReason,
			Message:            bundleMessage,
			ObservedGeneration: ext.GetGeneration(),
		})
		return
	}

	// Handle catalog data available: set conditions to True when deprecated, False when not.
	messages := slices.Concat(packageMessages, channelMessages, bundleMessages)
	if len(messages) > 0 {
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeDeprecated,
			Status:             metav1.ConditionTrue,
			Reason:             ocv1.ReasonDeprecated,
			Message:            strings.Join(messages, "\n"),
			ObservedGeneration: ext.GetGeneration(),
		})
	} else {
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeDeprecated,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ReasonNotDeprecated,
			Message:            "not deprecated",
			ObservedGeneration: ext.GetGeneration(),
		})
	}

	if len(packageMessages) > 0 {
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypePackageDeprecated,
			Status:             metav1.ConditionTrue,
			Reason:             ocv1.ReasonDeprecated,
			Message:            strings.Join(packageMessages, "\n"),
			ObservedGeneration: ext.GetGeneration(),
		})
	} else {
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypePackageDeprecated,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ReasonNotDeprecated,
			Message:            "package not deprecated",
			ObservedGeneration: ext.GetGeneration(),
		})
	}

	if len(channelMessages) > 0 {
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeChannelDeprecated,
			Status:             metav1.ConditionTrue,
			Reason:             ocv1.ReasonDeprecated,
			Message:            strings.Join(channelMessages, "\n"),
			ObservedGeneration: ext.GetGeneration(),
		})
	} else {
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeChannelDeprecated,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ReasonNotDeprecated,
			Message:            "channel not deprecated",
			ObservedGeneration: ext.GetGeneration(),
		})
	}

	// BundleDeprecated: Unknown when no bundle installed, True when deprecated, False when not
	if info.BundleStatus == metav1.ConditionUnknown {
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeBundleDeprecated,
			Status:             metav1.ConditionUnknown,
			Reason:             ocv1.ReasonAbsent,
			Message:            "no bundle installed yet",
			ObservedGeneration: ext.GetGeneration(),
		})
	} else if len(bundleMessages) > 0 {
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeBundleDeprecated,
			Status:             metav1.ConditionTrue,
			Reason:             ocv1.ReasonDeprecated,
			Message:            strings.Join(bundleMessages, "\n"),
			ObservedGeneration: ext.GetGeneration(),
		})
	} else {
		SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeBundleDeprecated,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ReasonNotDeprecated,
			Message:            "bundle not deprecated",
			ObservedGeneration: ext.GetGeneration(),
		})
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
				// Include channel deprecations if:
				// 1. No channels specified (channelSet empty) - any channel could be auto-selected
				// 2. The deprecated channel matches one of the specified channels
				if len(channelSet) == 0 || channelSet.Has(entry.Reference.Name) {
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
