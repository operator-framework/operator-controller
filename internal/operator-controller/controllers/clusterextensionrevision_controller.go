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
	"strings"
	"time"

	bsemver "github.com/blang/semver/v4"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	crhandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/operator-controller/resolve"
)

// ClusterExtensionRevisionReconciler reconciles ClusterExtension and ClusterCatalog objects
// to detect available upgrades and create ClusterExtensionRevision resources
type ClusterExtensionRevisionReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	Resolver              resolve.Resolver
	InstalledBundleGetter InstalledBundleGetter
}

// +kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensionrevisions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensionrevisions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensions,verbs=get;list;watch
// +kubebuilder:rbac:groups=olm.operatorframework.io,resources=clustercatalogs,verbs=get;list;watch

// Reconcile detects available upgrades for ClusterExtensions and manages ClusterExtensionRevision resources
func (r *ClusterExtensionRevisionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("clusterextensionrevision-controller")
	ctx = log.IntoContext(ctx, logger)

	logger.Info("reconcile starting")
	defer logger.Info("reconcile ending")

	// Get the specific ClusterExtension to reconcile
	var ext ocv1.ClusterExtension
	if err := r.Get(ctx, req.NamespacedName, &ext); err != nil {
		if apierrors.IsNotFound(err) {
			// ClusterExtension was deleted, clean up any associated revisions
			logger.Info("ClusterExtension not found, cleaning up revisions")
			return r.cleanupRevisionsForDeletedExtension(ctx, req.Name)
		}
		logger.Error(err, "failed to get ClusterExtension")
		return ctrl.Result{}, err
	}

	// Reconcile revisions for this specific ClusterExtension
	if err := r.reconcileExtensionRevisions(ctx, &ext); err != nil {
		logger.Error(err, "failed to reconcile revisions for ClusterExtension")
		return ctrl.Result{}, err
	}

	// Check for approved revisions and handle upgrades
	if err := r.handleApprovedRevision(ctx, &ext); err != nil {
		logger.Error(err, "failed to handle approved revisions for ClusterExtension")
		return ctrl.Result{}, err
	}

	// Requeue after a reasonable interval to periodically check for upgrades for this extension
	return ctrl.Result{RequeueAfter: 30 * time.Minute}, nil
}

// reconcileExtensionRevisions detects and manages available upgrade revisions for a single ClusterExtension
func (r *ClusterExtensionRevisionReconciler) reconcileExtensionRevisions(ctx context.Context, ext *ocv1.ClusterExtension) error {
	logger := log.FromContext(ctx).WithValues("clusterextension", ext.Name)

	// Skip if the extension is not installed yet
	if ext.Status.Install == nil {
		logger.V(4).Info("skipping ClusterExtension that is not yet installed")
		return nil
	}

	// Get the currently installed bundle
	installedBundle, err := r.InstalledBundleGetter.GetInstalledBundle(ctx, ext)
	if err != nil {
		return fmt.Errorf("failed to get installed bundle: %w", err)
	}
	if installedBundle == nil {
		logger.V(4).Info("no installed bundle found, skipping")
		return nil
	}

	// Find available upgrade using the resolver
	availableUpgrade, err := r.findAvailableUpgrade(ctx, ext, &installedBundle.BundleMetadata)
	if err != nil {
		return fmt.Errorf("failed to find available upgrades: %w", err)
	}

	if availableUpgrade == nil {
		// No upgrade available, nothing to do
		logger.V(4).Info("no upgrade available")
		return nil
	}

	// Clean old revision
	if err := r.cleanupObsoleteRevision(ctx, ext, availableUpgrade); err != nil {
		logger.Error(err, "failed to cleanup obsolete revisions")
	}

	if err := r.ensureRevision(ctx, ext, *availableUpgrade); err != nil {
		logger.Error(err, "failed to ensure revision", "version", availableUpgrade.Version)
	}

	return nil
}

// AvailableUpgrade represents an upgrade that's available for a ClusterExtension
type AvailableUpgrade struct {
	Bundle  *declcfg.Bundle
	Version *bsemver.Version
}

// findAvailableUpgrade uses the existing resolver logic to find available upgrades
func (r *ClusterExtensionRevisionReconciler) findAvailableUpgrade(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*AvailableUpgrade, error) {
	logger := log.FromContext(ctx)

	// If no version constraint is specified, find any available upgrade
	if ext.Spec.Source.Catalog == nil || ext.Spec.Source.Catalog.Version == "" {
		return r.findAnyAvailableUpgrade(ctx, ext, installedBundle)
	}

	versionConstraint := ext.Spec.Source.Catalog.Version

	// Check if this is a pinned version (exact version match)
	if isPinnedVersion(versionConstraint, installedBundle.Version) {
		logger.V(4).Info("version is pinned, no upgrades will be available", "version", versionConstraint, "installed", installedBundle.Version)
		return nil, nil // No upgrades for pinned versions
	}

	// For version ranges, find upgrades within the range
	return r.findUpgradeInVersionRange(ctx, ext, installedBundle)
}

// isPinnedVersion checks if the version constraint represents a pinned version
// A pinned version is an exact version match without operators (e.g., "1.2.3" but not ">=1.2.3")
func isPinnedVersion(versionConstraint, installedVersion string) bool {
	// Check if the constraint only allows exactly one version
	// This is a heuristic - we consider it pinned if:
	// 1. The constraint string doesn't contain operators like >=, <=, >, <, ~, ^, ||
	// 2. The constraint matches exactly the installed version
	hasOperators := strings.ContainsAny(versionConstraint, "><=~^|")
	if hasOperators {
		return false
	}

	// For a simple version string without operators, we consider it pinned
	// if it exactly matches the installed version
	return strings.TrimSpace(versionConstraint) == strings.TrimSpace(installedVersion)
}

// findAnyAvailableUpgrade finds any available upgrade without version constraints
func (r *ClusterExtensionRevisionReconciler) findAnyAvailableUpgrade(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*AvailableUpgrade, error) {
	logger := log.FromContext(ctx)

	// Create a modified ClusterExtension spec that removes version constraints
	// to find all available bundles, not just those matching the current version spec
	extForResolution := ext.DeepCopy()
	if extForResolution.Spec.Source.Catalog != nil {
		// Remove version constraint to find all available versions
		extForResolution.Spec.Source.Catalog.Version = ""
	}

	// Use the resolver to find all available bundles
	resolvedBundle, resolvedVersion, _, err := r.Resolver.Resolve(ctx, extForResolution, installedBundle)
	if err != nil {
		logger.V(4).Info("no bundles resolved", "error", err)
		return nil, nil // No error, just no upgrades available
	}

	// Check if the resolved version is actually an upgrade
	installedVersion, err := bsemver.ParseTolerant(installedBundle.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to parse installed version %q: %w", installedBundle.Version, err)
	}

	if !resolvedVersion.GT(installedVersion) {
		logger.V(4).Info("resolved version is not an upgrade", "resolved", resolvedVersion.String(), "installed", installedVersion.String())
		return nil, nil // No upgrade available
	}

	return &AvailableUpgrade{
		Bundle:  resolvedBundle,
		Version: resolvedVersion,
	}, nil
}

// findUpgradeInVersionRange finds upgrades within the specified version range
// The version range is taken from ext.Spec.Source.Catalog.Version
func (r *ClusterExtensionRevisionReconciler) findUpgradeInVersionRange(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*AvailableUpgrade, error) {
	logger := log.FromContext(ctx)

	versionConstraint := ext.Spec.Source.Catalog.Version

	// Use the resolver to find bundles within the version range
	// The resolver will respect the version constraint in ext.Spec.Source.Catalog.Version
	resolvedBundle, resolvedVersion, _, err := r.Resolver.Resolve(ctx, ext, installedBundle)
	if err != nil {
		logger.V(4).Info("no bundles resolved within version range", "versionRange", versionConstraint, "error", err)
		return nil, nil // No error, just no upgrades available
	}

	// Check if the resolved version is actually an upgrade
	installedVersion, err := bsemver.ParseTolerant(installedBundle.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to parse installed version %q: %w", installedBundle.Version, err)
	}

	if !resolvedVersion.GT(installedVersion) {
		logger.V(4).Info("resolved version within range is not an upgrade", "resolved", resolvedVersion.String(), "installed", installedVersion.String(), "versionRange", versionConstraint)
		return nil, nil // No upgrade available within range
	}

	logger.V(4).Info("found upgrade within version range", "resolved", resolvedVersion.String(), "installed", installedVersion.String(), "versionRange", versionConstraint)

	return &AvailableUpgrade{
		Bundle:  resolvedBundle,
		Version: resolvedVersion,
	}, nil
}

// ensureRevision creates or updates a ClusterExtensionRevision for an available upgrade
func (r *ClusterExtensionRevisionReconciler) ensureRevision(ctx context.Context, ext *ocv1.ClusterExtension, upgrade AvailableUpgrade) error {
	logger := log.FromContext(ctx)

	// Generate a name for the revision
	revisionName := fmt.Sprintf("%s-%s", ext.Name, upgrade.Version.String())

	// Check if revision already exists
	var existingRevision ocv1.ClusterExtensionRevision
	err := r.Get(ctx, types.NamespacedName{Name: revisionName}, &existingRevision)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get existing revision: %w", err)
	}

	now := metav1.Now()
	bundleMetadata := bundleutil.MetadataFor(upgrade.Bundle.Name, *upgrade.Version)

	if apierrors.IsNotFound(err) {
		// Create new revision
		revision := &ocv1.ClusterExtensionRevision{
			ObjectMeta: metav1.ObjectMeta{
				Name: revisionName,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: ext.APIVersion,
						Kind:       ext.Kind,
						Name:       ext.Name,
						UID:        ext.UID,
					},
				},
			},
			Spec: ocv1.ClusterExtensionRevisionSpec{
				ClusterExtensionRef: ocv1.ClusterExtensionReference{
					Name: ext.Name,
				},
				Version:        upgrade.Version.String(),
				BundleMetadata: bundleMetadata,
				AvailableSince: now,
			},
		}

		if err := r.Create(ctx, revision); err != nil {
			return fmt.Errorf("failed to create revision: %w", err)
		}

		logger.Info("created new ClusterExtensionRevision", "revision", revisionName, "version", upgrade.Version.String())
	} else {
		// Update existing revision if needed
		// For now, we mainly need to ensure the spec is current
		// The AvailableSince timestamp should remain unchanged
		logger.V(4).Info("revision already exists", "revision", revisionName)
	}

	return nil
}

// cleanupObsoleteRevisions removes ClusterExtensionRevision resources that are no longer valid
func (r *ClusterExtensionRevisionReconciler) cleanupObsoleteRevision(ctx context.Context, ext *ocv1.ClusterExtension, currentUpgrade *AvailableUpgrade) error {
	logger := log.FromContext(ctx)

	// List all existing revisions for this ClusterExtension
	var revisions ocv1.ClusterExtensionRevisionList
	if err := r.List(ctx, &revisions, client.MatchingFields{"spec.clusterExtensionRef.name": ext.Name}); err != nil {
		return fmt.Errorf("failed to list existing revisions: %w", err)
	}

	// Delete revisions that are no longer available
	for _, revision := range revisions.Items {
		if revision.Spec.Version != currentUpgrade.Version.String() {
			logger.Info("deleting obsolete revision", "revision", revision.Name, "version", revision.Spec.Version)
			if err := r.Delete(ctx, &revision); err != nil {
				logger.Error(err, "failed to delete obsolete revision", "revision", revision.Name)
			}
		}
	}

	return nil
}

// cleanupRevisionsForDeletedExtension removes all ClusterExtensionRevision resources for a deleted ClusterExtension
func (r *ClusterExtensionRevisionReconciler) cleanupRevisionsForDeletedExtension(ctx context.Context, extensionName string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// List all revisions for this ClusterExtension
	var revisions ocv1.ClusterExtensionRevisionList
	if err := r.List(ctx, &revisions, client.MatchingFields{"spec.clusterExtensionRef.name": extensionName}); err != nil {
		logger.Error(err, "failed to list revisions for deleted ClusterExtension", "extensionName", extensionName)
		return ctrl.Result{}, err
	}

	// Delete all revisions for the deleted extension
	for _, revision := range revisions.Items {
		logger.Info("deleting revision for deleted ClusterExtension", "revision", revision.Name, "extensionName", extensionName)
		if err := r.Delete(ctx, &revision); err != nil {
			logger.Error(err, "failed to delete revision for deleted ClusterExtension", "revision", revision.Name)
		}
	}

	return ctrl.Result{}, nil
}

// handleApprovedRevision handles the approved ClusterExtensionRevision resource and upgrades the corresponding ClusterExtension
func (r *ClusterExtensionRevisionReconciler) handleApprovedRevision(ctx context.Context, ext *ocv1.ClusterExtension) error {
	logger := log.FromContext(ctx).WithValues("clusterextension", ext.Name)

	// List all revisions for this ClusterExtension (should be at most one)
	var revisions ocv1.ClusterExtensionRevisionList
	if err := r.List(ctx, &revisions, client.MatchingFields{"spec.clusterExtensionRef.name": ext.Name}); err != nil {
		return fmt.Errorf("failed to list revisions: %w", err)
	}

	// Find the approved revision (there should be at most one)
	var approvedRevision *ocv1.ClusterExtensionRevision
	for _, revision := range revisions.Items {
		if revision.Spec.Approved {
			if approvedRevision != nil {
				// This shouldn't happen given our design constraint, but let's be defensive
				logger.Error(nil, "multiple approved revisions found, this should not happen",
					"existing", approvedRevision.Name, "duplicate", revision.Name)
				continue
			}
			approvedRevision = &revision
		}
	}

	// If no approved revision found, nothing to do
	if approvedRevision == nil {
		return nil
	}

	// Set approvedAt timestamp if not already set
	if approvedRevision.Spec.ApprovedAt == nil {
		now := metav1.Now()
		approvedRevision.Spec.ApprovedAt = &now
		if err := r.Update(ctx, approvedRevision); err != nil {
			return fmt.Errorf("failed to update revision with approval timestamp: %w", err)
		}
		logger.Info("set approval timestamp for revision", "revision", approvedRevision.Name, "approvedAt", now)
	}

	logger.Info("handling approved revision", "revision", approvedRevision.Name, "version", approvedRevision.Spec.Version)

	// Upgrade the ClusterExtension
	if err := r.upgradeClusterExtension(ctx, ext, approvedRevision); err != nil {
		return fmt.Errorf("failed to upgrade ClusterExtension: %w", err)
	}

	return nil
}

// upgradeClusterExtension handles the approved revision lifecycle
func (r *ClusterExtensionRevisionReconciler) upgradeClusterExtension(ctx context.Context, ext *ocv1.ClusterExtension, revision *ocv1.ClusterExtensionRevision) error {
	logger := log.FromContext(ctx).WithValues("clusterextension", ext.Name, "revision", revision.Name)

	// Check if the upgrade has already been completed
	if ext.Status.Install != nil && ext.Status.Install.Bundle.Version == revision.Spec.Version {
		// Upgrade completed, clean up the revision
		logger.Info("upgrade completed, cleaning up revision", "version", revision.Spec.Version)

		// Delete the completed revision
		if err := r.Delete(ctx, revision); err != nil {
			return fmt.Errorf("failed to delete completed revision: %w", err)
		}

		return nil
	}

	// The approved revision exists and upgrade hasn't completed yet
	// The ClusterExtension controller will detect this approved revision during its reconcile
	logger.Info("approved revision ready for upgrade", "version", revision.Spec.Version)
	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ClusterExtensionRevisionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Add index for efficient lookups of revisions by ClusterExtension name
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&ocv1.ClusterExtensionRevision{},
		"spec.clusterExtensionRef.name",
		func(rawObj client.Object) []string {
			revision := rawObj.(*ocv1.ClusterExtensionRevision)
			return []string{revision.Spec.ClusterExtensionRef.Name}
		},
	); err != nil {
		return err
	}

	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&ocv1.ClusterExtension{}).
		Owns(&ocv1.ClusterExtensionRevision{}).
		Named("clusterextensionrevision-controller").
		Watches(&ocv1.ClusterCatalog{},
			crhandler.EnqueueRequestsFromMapFunc(r.catalogToExtensionRequests),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(ue event.UpdateEvent) bool {
					// Only trigger when catalog content changes (similar to ClusterExtension controller)
					oldCatalog, isOldCatalog := ue.ObjectOld.(*ocv1.ClusterCatalog)
					newCatalog, isNewCatalog := ue.ObjectNew.(*ocv1.ClusterCatalog)

					if !isOldCatalog || !isNewCatalog {
						return true
					}

					if oldCatalog.Status.ResolvedSource != nil && newCatalog.Status.ResolvedSource != nil {
						if oldCatalog.Status.ResolvedSource.Image != nil && newCatalog.Status.ResolvedSource.Image != nil {
							return oldCatalog.Status.ResolvedSource.Image.Ref != newCatalog.Status.ResolvedSource.Image.Ref
						}
					}
					return true
				},
			})).
		Watches(&ocv1.ClusterExtensionRevision{},
			crhandler.EnqueueRequestsFromMapFunc(r.revisionToExtensionRequests),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(ue event.UpdateEvent) bool {
					// Only trigger when the approved field changes from false to true
					oldRevision, isOldRevision := ue.ObjectOld.(*ocv1.ClusterExtensionRevision)
					newRevision, isNewRevision := ue.ObjectNew.(*ocv1.ClusterExtensionRevision)

					if !isOldRevision || !isNewRevision {
						return false
					}

					// Trigger reconcile when approved changes from false to true
					return !oldRevision.Spec.Approved && newRevision.Spec.Approved
				},
			})).
		Build(r)
	return err
}

// catalogToExtensionRequests generates reconcile requests for all ClusterExtensions when a catalog changes
func (r *ClusterExtensionRevisionReconciler) catalogToExtensionRequests(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	// List all ClusterExtensions and create reconcile requests for each one
	// This follows the same pattern as the existing ClusterExtension controller
	clusterExtensions := metav1.PartialObjectMetadataList{}
	clusterExtensions.SetGroupVersionKind(ocv1.GroupVersion.WithKind("ClusterExtensionList"))
	err := r.List(ctx, &clusterExtensions)
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

	logger.V(4).Info("enqueuing reconcile requests for catalog change", "numExtensions", len(requests))
	return requests
}

// revisionToExtensionRequests generates reconcile requests for ClusterExtensions when a revision is approved
func (r *ClusterExtensionRevisionReconciler) revisionToExtensionRequests(ctx context.Context, obj client.Object) []reconcile.Request {
	revision, ok := obj.(*ocv1.ClusterExtensionRevision)
	if !ok {
		return nil
	}

	// Return a reconcile request for the ClusterExtension referenced by this revision
	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name: revision.Spec.ClusterExtensionRef.Name,
			},
		},
	}
}
