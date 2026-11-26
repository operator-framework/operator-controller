//go:build !standard

package controllers

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
)

// archivalFieldOwner is the server-side apply field manager that owns
// spec.lifecycleState after the deployment controller archives a revision.
const archivalFieldOwner = "olm.operatorframework.io/cluster-object-deployment-controller"

// archivePreviousRevisions requests archival below the highest completed, active,
// owned revision. Completion is historical: neither current health nor a later
// pending rollout invalidates the boundary. revisions is the deployment's group and
// must be sorted ascending by revision number (as listRevisions does): it is walked
// from the newest revision down, so the first eligible completed revision is the
// boundary and eligible revisions below it are archived. Only members controlled by
// cod are considered. Successful archival writes are reflected in place.
func (c *ClusterObjectDeploymentReconciler) archivePreviousRevisions(ctx context.Context, cod *ocv1.ClusterObjectDeployment, revisions []ocv1.ClusterObjectSet) error {
	var boundary int64
	var errs []error
	for i := len(revisions) - 1; i >= 0; i-- {
		rev := &revisions[i]
		if !isControlledByDeployment(cod, rev) || !rev.DeletionTimestamp.IsZero() ||
			rev.Spec.LifecycleState != ocv1.ClusterObjectSetLifecycleStateActive {
			continue
		}
		if boundary == 0 {
			if !rev.Status.CompletedAt.IsZero() {
				boundary = rev.Spec.Revision
			}
			continue
		}
		// Never archive a sibling that shares the boundary's revision number.
		if rev.Spec.Revision >= boundary {
			continue
		}
		if err := c.archiveRevision(ctx, rev); err != nil {
			errs = append(errs, fmt.Errorf("archiving revision %q: %w", rev.Name, err))
		}
	}
	return errors.Join(errs...)
}

// archiveRevision applies the Archived lifecycle state to rev, updating rev in place
// on success. It returns an error if the API server did not persist Archived.
func (c *ClusterObjectDeploymentReconciler) archiveRevision(ctx context.Context, rev *ocv1.ClusterObjectSet) error {
	// Both guards are checked by the API server as part of this one apply: a
	// resourceVersion mismatch is a Conflict, and a UID mismatch is rejected
	// because metadata.uid is immutable. If the revision has disappeared, the
	// UID also prevents server-side apply from recreating it (Conflict).
	// No read-before-write check can protect against a concurrent owner change
	// or a same-name replacement. Never refresh these guards and blindly retry.
	// A conflict means rev changed or was deleted after it was observed; that
	// change triggers another reconcile, which acts on the current state.
	archival := ocv1ac.ClusterObjectSet(rev.Name).
		WithUID(rev.UID).
		WithResourceVersion(rev.ResourceVersion).
		WithSpec(ocv1ac.ClusterObjectSetSpec().
			WithLifecycleState(ocv1.ClusterObjectSetLifecycleStateArchived))
	// ForceOwnership is required because spec.lifecycleState is initially owned
	// by the manager that created the revision.
	if err := c.Client.Apply(ctx, archival, client.FieldOwner(archivalFieldOwner), client.ForceOwnership); err != nil {
		return err
	}
	// Apply decodes the stored object into archival. Reflect what the API server
	// persisted rather than what was requested: admission may have kept the revision
	// Active, and treating it as Archived would hide it from availability and leave
	// the failed archival unreported.
	if archival.ResourceVersion != nil {
		rev.ResourceVersion = *archival.ResourceVersion
	}
	if archival.Spec == nil || archival.Spec.LifecycleState == nil ||
		*archival.Spec.LifecycleState != ocv1.ClusterObjectSetLifecycleStateArchived {
		return fmt.Errorf("lifecycleState was not persisted as %s", ocv1.ClusterObjectSetLifecycleStateArchived)
	}
	rev.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
	return nil
}
