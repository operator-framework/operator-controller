package applier

import (
	"context"
	"errors"
	"fmt"

	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/revisions"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

// BoxcutterStorageMigrator migrates ClusterExtensions from Helm-based storage to
// ClusterObjectSet storage, enabling upgrades from older operator-controller versions.
// TODO(COD): Adapt Helm storage migration for ClusterObjectDeployment-backed
// installations and remove direct COS discovery when that migration path is in place.
type BoxcutterStorageMigrator struct {
	ActionClientGetter helmclient.ActionClientGetter
	RevisionGenerator  RevisionGenerator
	Client             boxcutterStorageMigratorClient
	Scheme             *runtime.Scheme
	FieldOwner         string
}

type boxcutterStorageMigratorClient interface {
	Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	Status() client.StatusWriter
}

// Migrate creates a ClusterObjectSet from an existing Helm release if no revisions exist yet.
// The migration is idempotent and skipped if revisions already exist or no Helm release is found.
func (m *BoxcutterStorageMigrator) Migrate(ctx context.Context, ext *ocv1.ClusterExtension, objectLabels map[string]string) error {
	// Managed namespace mode (spec.namespace empty) means this is a new-style extension
	// that never had a Helm release, so there is nothing to migrate. The previous CRD
	// required a non-empty namespace, while the current CRD prevents an existing
	// namespace from being removed. Consequently, an extension with an omitted namespace
	// must have been created after managed namespace mode was introduced.
	if ext.Spec.Namespace == "" {
		return nil
	}
	existingRevisions, err := revisions.ListForGroup(ctx, m.Client, ext.Name)
	if err != nil {
		return fmt.Errorf("listing ClusterObjectSets before attempting migration: %w", err)
	}
	if len(existingRevisions) != 0 {
		return m.ensureMigratedRevisionStatus(ctx, existingRevisions)
	}

	ac, err := m.ActionClientGetter.ActionClientFor(ctx, ext)
	if err != nil {
		return err
	}

	helmRelease, err := ac.Get(ext.GetName())
	if errors.Is(err, driver.ErrReleaseNotFound) {
		// no Helm Release -> no prior installation.
		return nil
	}
	if err != nil {
		return err
	}

	// Only migrate from a Helm release that represents a deployed, working installation.
	// If the latest revision is not deployed (e.g. FAILED), look through the history and
	// select the most-recent deployed release instead.
	if helmRelease == nil || helmRelease.Info == nil || helmRelease.Info.Status != release.StatusDeployed {
		var err error
		helmRelease, err = m.findLatestDeployedRelease(ac, ext.GetName())
		if err != nil {
			return err
		}
		if helmRelease == nil {
			// No deployed release found in history - skip migration. The ClusterExtension
			// controller will handle this via normal rollout.
			return nil
		}
	}

	rev, err := m.RevisionGenerator.GenerateRevisionFromHelmRelease(ctx, helmRelease, ext, objectLabels)
	if err != nil {
		return err
	}

	// Mark this revision as migrated from Helm so we can distinguish it from
	// normal Boxcutter revisions. This label is critical for ensuring we only
	// set Succeeded=True status on actually-migrated revisions, not on revision 1
	// created during normal Boxcutter operation.
	rev.WithLabels(map[string]string{labels.MigratedFromHelmKey: "true"})

	// Set ownerReference for proper garbage collection when the ClusterExtension is deleted.
	gvk, err := apiutil.GVKForObject(ext, m.Scheme)
	if err != nil {
		return fmt.Errorf("get GVK for owner: %w", err)
	}
	rev.WithOwnerReferences(metav1ac.OwnerReference().
		WithAPIVersion(gvk.GroupVersion().String()).
		WithKind(gvk.Kind).
		WithName(ext.Name).
		WithUID(ext.UID).
		WithBlockOwnerDeletion(true).
		WithController(true))

	if err := m.Client.Apply(ctx, rev, client.FieldOwner(m.FieldOwner), client.ForceOwnership); err != nil {
		return err
	}

	// Set initial status on the migrated revision to mark it as succeeded.
	//
	// The revision must have a Succeeded=True status condition immediately after creation.
	//
	// A revision is only considered "Installed" (vs "RollingOut") when it has this condition.
	// Without it, the system cannot determine what version is currently installed, which breaks:
	//   - Version resolution (can't compute upgrade paths from unknown starting point)
	//   - Status reporting (installed bundle appears as nil)
	//   - Subsequent upgrades (resolution fails without knowing current version)
	//
	// While the ClusterObjectSet controller would eventually reconcile and set this status,
	// that creates a timing gap where the ClusterExtension reconciliation happens before the status
	// is set, causing failures during the OLM upgrade window.
	//
	// Since we're creating this revision from a successfully deployed Helm release, we know it
	// represents a working installation and can safely mark it as succeeded immediately.
	return m.ensureRevisionStatus(ctx, *rev.GetName())
}

// ensureMigratedRevisionStatus checks if revision 1 exists and needs its status set.
// This handles the case where revision creation succeeded but status update failed.
// Returns nil if no action is needed.
func (m *BoxcutterStorageMigrator) ensureMigratedRevisionStatus(ctx context.Context, revisions []ocv1.ClusterObjectSet) error {
	for i := range revisions {
		if revisions[i].Spec.Revision != 1 {
			continue
		}
		// Skip if already completed - status is already set correctly.
		if !revisions[i].Status.CompletedAt.IsZero() {
			return nil
		}
		// Ensure revision 1 status is set correctly, including for previously migrated
		// revisions that may not carry the MigratedFromHelm label.
		return m.ensureRevisionStatus(ctx, revisions[i].Name)
	}
	// No revision 1 found - migration not applicable (revisions created by normal operation).
	return nil
}

// findLatestDeployedRelease searches the Helm release history for the most recent deployed release.
// Returns nil if no deployed release is found.
func (m *BoxcutterStorageMigrator) findLatestDeployedRelease(ac helmclient.ActionInterface, name string) (*release.Release, error) {
	history, err := ac.History(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		// no Helm Release history -> no prior installation.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var latestDeployed *release.Release
	for _, rel := range history {
		if rel == nil || rel.Info == nil {
			continue
		}
		if rel.Info.Status != release.StatusDeployed {
			continue
		}
		if latestDeployed == nil || rel.Version > latestDeployed.Version {
			latestDeployed = rel
		}
	}

	return latestDeployed, nil
}

// ensureRevisionStatus ensures the revision has completedAt set, marking it as
// installed. Returns nil if the status is already set or after successfully setting it.
// Only sets status on revisions that were actually migrated from Helm (marked with MigratedFromHelmKey label).
func (m *BoxcutterStorageMigrator) ensureRevisionStatus(ctx context.Context, name string) error {
	rev := &ocv1.ClusterObjectSet{}
	if err := m.Client.Get(ctx, client.ObjectKey{Name: name}, rev); err != nil {
		return fmt.Errorf("getting existing revision for status check: %w", err)
	}

	// Only set status if this revision was actually migrated from Helm.
	// This prevents us from incorrectly marking normal Boxcutter revision 1 as completed
	// when it's still in progress.
	if rev.Labels[labels.MigratedFromHelmKey] != "true" {
		return nil
	}

	// Check if completedAt is already set.
	if !rev.Status.CompletedAt.IsZero() {
		return nil
	}

	// Since we're migrating from a successfully deployed Helm release, the revision
	// represents a working installation. Record completedAt so the ClusterExtension
	// controller treats it as installed. The original ready time is not recoverable,
	// so we use the migration time.
	rev.Status.CompletedAt = metav1.Now()

	if err := m.Client.Status().Update(ctx, rev); err != nil {
		return fmt.Errorf("updating migrated revision status: %w", err)
	}

	return nil
}
