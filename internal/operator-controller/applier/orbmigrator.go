package applier

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	orbv1alpha1 "github.com/joelanford/orb-operator/api/v1alpha1"
	orbac "github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	orb "github.com/operator-framework/operator-controller/internal/operator-controller/applier/orb"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	"github.com/operator-framework/operator-controller/internal/shared/util/cache"
)

// orbMigrationPhaseName is the name given to the assertion-free phase(s) of the
// adopting ClusterObjectSet built from a Helm release.
const orbMigrationPhaseName = "migrate"

// orbAdoptionRequeueInterval is how often the migration step re-checks whether
// the adopting revision has completed while gating the pipeline. The controller
// does not watch ClusterObjectSet, so we poll for status.completedAt.
const orbAdoptionRequeueInterval = 5 * time.Second

// OrbClusterObjectSetGenerator builds an orb ClusterObjectSet apply
// configuration from a deployed Helm release, for adoption into the orb runtime.
type OrbClusterObjectSetGenerator interface {
	GenerateRevisionFromHelmRelease(
		ctx context.Context,
		helmRelease *release.Release,
		ext *ocv1.ClusterExtension,
		objectLabels map[string]string,
	) (*orbac.ClusterObjectSetApplyConfiguration, error)
}

// SimpleOrbRevisionGenerator builds an adopting ClusterObjectSet from a Helm
// release. Unlike the COD generator, it places every object in a single
// assertion-free phase (chunked only to satisfy the per-phase object cap) and
// uses collisionProtection None, mimicking Helm's "apply everything at once, no
// ordering, no readiness gating" semantics so the revision can quickly take
// ownership of the already-running Helm-managed objects.
type SimpleOrbRevisionGenerator struct{}

func (g *SimpleOrbRevisionGenerator) GenerateRevisionFromHelmRelease(
	ctx context.Context,
	helmRelease *release.Release,
	ext *ocv1.ClusterExtension,
	objectLabels map[string]string,
) (*orbac.ClusterObjectSetApplyConfiguration, error) {
	docs := splitManifestDocuments(helmRelease.Manifest)
	phaseObjects := make([]*orbac.PhaseObjectApplyConfiguration, 0, len(docs))
	for _, doc := range docs {
		obj := unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			return nil, fmt.Errorf("unmarshaling Helm manifest object: %w", err)
		}
		obj.SetLabels(mergeStringMaps(obj.GetLabels(), objectLabels))

		// Memory optimization: strip large annotations.
		// Note: ApplyStripAnnotationsTransform never returns an error in practice.
		_ = cache.ApplyStripAnnotationsTransform(&obj)
		sanitizedUnstructured(ctx, &obj)

		annotationUpdates := map[string]string{}
		if v := helmRelease.Labels[labels.BundleVersionKey]; v != "" {
			annotationUpdates[labels.BundleVersionKey] = v
		}
		if v, ok := helmRelease.Labels[labels.BundleReleaseKey]; ok {
			annotationUpdates[labels.BundleReleaseKey] = v
		}
		if v := helmRelease.Labels[labels.PackageNameKey]; v != "" {
			annotationUpdates[labels.PackageNameKey] = v
		}
		if len(annotationUpdates) > 0 {
			obj.SetAnnotations(mergeStringMaps(obj.GetAnnotations(), annotationUpdates))
		}

		raw, err := json.Marshal(obj.Object)
		if err != nil {
			return nil, fmt.Errorf("marshaling Helm manifest object to JSON: %w", err)
		}
		// No assertions: adoption of already-running objects must not stall on
		// per-object readiness conditions.
		phaseObjects = append(phaseObjects, orbac.PhaseObject().
			WithObject(runtime.RawExtension{Raw: raw}))
	}

	revisionAnnotations := map[string]string{
		labels.BundleNameKey:      helmRelease.Labels[labels.BundleNameKey],
		labels.PackageNameKey:     helmRelease.Labels[labels.PackageNameKey],
		labels.BundleVersionKey:   helmRelease.Labels[labels.BundleVersionKey],
		labels.BundleReferenceKey: helmRelease.Labels[labels.BundleReferenceKey],
	}
	if v, ok := helmRelease.Labels[labels.BundleReleaseKey]; ok {
		revisionAnnotations[labels.BundleReleaseKey] = v
	}

	spec := orbac.ClusterObjectSetSpec().
		WithGroup(ext.Name).
		WithLifecycleState(orbv1alpha1.LifecycleStateActive).
		// None so the revision adopts the still-Helm-owned objects. Only the
		// first adoption needs None; every later (COD-owned) revision uses
		// Prevent and takes over via orb's sibling handoff.
		WithCollisionProtection(orbv1alpha1.CollisionProtectionNone).
		WithPhases(buildOrbMigrationPhases(phaseObjects)...)

	// Owner labels let the externalizer's slices and the migrator's revision
	// listing find this revision by the same selector used elsewhere.
	// Deliberately no LabelTemplateHash ("orb.operatorframework.io/template-hash"):
	// leaving it unset guarantees a mismatch against the COD's Prevent template
	// hash, so the COD controller stamps the Prevent revision rather than forcing
	// this adopting revision back to Prevent via ensureFieldOwnership.
	return orbac.ClusterObjectSet("").
		WithAnnotations(revisionAnnotations).
		WithLabels(map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.Name,
		}).
		WithSpec(spec), nil
}

// buildOrbMigrationPhases places all objects in a single assertion-free phase,
// splitting into additional no-assertion phases only when the object count
// exceeds the per-phase cap. The split is purely to satisfy the limit, not for
// ordering.
func buildOrbMigrationPhases(objs []*orbac.PhaseObjectApplyConfiguration) []*orbac.PhaseApplyConfiguration {
	chunks := slices.Collect(slices.Chunk(objs, maxObjectsPerPhase))
	multiChunk := len(chunks) > 1
	phases := make([]*orbac.PhaseApplyConfiguration, 0, len(chunks))
	for i, chunk := range chunks {
		name := orbMigrationPhaseName
		if multiChunk {
			name = fmt.Sprintf("%s-%d", orbMigrationPhaseName, i+1)
		}
		phases = append(phases, orbac.Phase().WithName(name).WithObjects(chunk...))
	}
	return phases
}

// OrbStorageMigrator migrates a ClusterExtension installed under the legacy Helm
// runtime to the orb runtime without an uninstall/reinstall. It builds a first
// adopting ClusterObjectSet revision (collisionProtection None) directly from
// the deployed Helm release so the orb controller adopts the existing
// Helm-managed objects, then - once that revision completes - deletes the Helm
// release storage so the normal apply path takes over via an orb COD.
type OrbStorageMigrator struct {
	ActionClientGetter helmclient.ActionClientGetter
	RevisionGenerator  OrbClusterObjectSetGenerator
	Client             orbStorageMigratorClient
	Scheme             *runtime.Scheme
	FieldOwner         string
}

type orbStorageMigratorClient interface {
	Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

// Migrate ensures the adopting revision exists for a deployed Helm release and
// reports whether the pipeline should be gated.
//
//   - No deployed Helm release: nothing to migrate (fresh install, or migration
//     already finished). Returns a nil result so the pipeline proceeds.
//   - Deployed Helm release present, adoption not yet complete: ensures the
//     adopting revision and returns a requeue result to gate the pipeline before
//     the COD is created.
//   - Deployed Helm release present, adoption complete: deletes the Helm release
//     storage (bookkeeping secrets only) and returns a nil result.
func (m *OrbStorageMigrator) Migrate(ctx context.Context, ext *ocv1.ClusterExtension, objectLabels map[string]string) (*ctrl.Result, error) {
	l := log.FromContext(ctx)

	ac, err := m.ActionClientGetter.ActionClientFor(ctx, ext)
	if err != nil {
		return nil, err
	}

	helmRelease, err := m.findDeployedRelease(ac, ext.GetName())
	if err != nil {
		return nil, err
	}
	if helmRelease == nil {
		// No deployed Helm release -> nothing to migrate.
		return nil, nil
	}

	existing, err := m.listRevisions(ctx, ext.GetName())
	if err != nil {
		return nil, err
	}

	adopting, err := m.ensureAdoptingRevision(ctx, ext, helmRelease, objectLabels, existing)
	if err != nil {
		return nil, err
	}

	// Gate the pipeline while adoption is in progress: the COD must not be
	// created (which would stamp a premature Prevent revision that collides with
	// the still-unowned objects) until the adopting revision has completed.
	if adopting.Status.CompletedAt == nil {
		l.Info("waiting for adopting revision to complete before releasing Helm storage", "revision", adopting.Name)
		return &ctrl.Result{RequeueAfter: orbAdoptionRequeueInterval}, nil
	}

	// Adoption succeeded: delete only the Helm bookkeeping secrets (not a Helm
	// uninstall - the managed objects are now orb-adopted and must not be torn
	// down). From here migration becomes a no-op and the normal pipeline runs.
	l.Info("adopting revision complete, deleting Helm release storage", "revision", adopting.Name)
	if err := m.deleteHelmReleaseStorage(ac, ext.GetName()); err != nil {
		return nil, fmt.Errorf("deleting Helm release storage: %w", err)
	}
	return nil, nil
}

// findDeployedRelease returns the most-recent deployed Helm release, falling
// back through history when the latest release is not deployed. It returns
// (nil, nil) when there is no deployed release (nothing to migrate).
func (m *OrbStorageMigrator) findDeployedRelease(ac helmclient.ActionInterface, name string) (*release.Release, error) {
	rel, err := ac.Get(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if rel != nil && rel.Info != nil && rel.Info.Status == release.StatusDeployed {
		return rel, nil
	}
	return m.findLatestDeployedRelease(ac, name)
}

func (m *OrbStorageMigrator) findLatestDeployedRelease(ac helmclient.ActionInterface, name string) (*release.Release, error) {
	history, err := ac.History(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var latest *release.Release
	for _, rel := range history {
		if rel == nil || rel.Info == nil || rel.Info.Status != release.StatusDeployed {
			continue
		}
		if latest == nil || rel.Version > latest.Version {
			latest = rel
		}
	}
	return latest, nil
}

func (m *OrbStorageMigrator) listRevisions(ctx context.Context, extName string) ([]orbv1alpha1.ClusterObjectSet, error) {
	list := &orbv1alpha1.ClusterObjectSetList{}
	if err := m.Client.List(ctx, list, client.MatchingLabels{labels.OwnerNameKey: extName}); err != nil {
		return nil, fmt.Errorf("listing revisions: %w", err)
	}
	slices.SortFunc(list.Items, func(a, b orbv1alpha1.ClusterObjectSet) int {
		return cmp.Compare(a.Spec.Revision, b.Spec.Revision)
	})
	return list.Items, nil
}

// ensureAdoptingRevision reuses the latest existing revision when the desired
// spec is already reflected there (idempotent), otherwise creates a new revision
// with the next revision number. It never updates an existing revision in place
// (COS spec is immutable). The returned ClusterObjectSet is used only to read
// status.completedAt: a freshly-created revision reports a nil completedAt.
func (m *OrbStorageMigrator) ensureAdoptingRevision(
	ctx context.Context,
	ext *ocv1.ClusterExtension,
	helmRelease *release.Release,
	objectLabels map[string]string,
	existing []orbv1alpha1.ClusterObjectSet,
) (*orbv1alpha1.ClusterObjectSet, error) {
	if len(existing) > 0 {
		latest := &existing[len(existing)-1]
		match, err := m.desiredMatchesRevision(ctx, ext, helmRelease, objectLabels, latest)
		if err != nil {
			return nil, err
		}
		if match {
			return latest, nil
		}
	}

	revNum := nextOrbRevisionNumber(existing)
	name := fmt.Sprintf("%s-%d", ext.Name, revNum)

	desired, err := m.buildDesired(ctx, ext, helmRelease, objectLabels, name, revNum)
	if err != nil {
		return nil, err
	}

	// Externalize before creating so an oversized Helm release does not exceed
	// etcd limits. The slices must be applied before the revision, since its
	// objectRefs point to them.
	externalized, slices, err := orb.ExternalizeCOS(desired)
	if err != nil {
		return nil, fmt.Errorf("externalizing adopting ClusterObjectSet: %w", err)
	}
	for _, slice := range slices {
		if err := m.Client.Apply(ctx, slice, client.FieldOwner(m.FieldOwner), client.ForceOwnership); err != nil {
			return nil, fmt.Errorf("applying ClusterObjectSlice: %w", err)
		}
	}
	if err := m.Client.Apply(ctx, externalized, client.FieldOwner(m.FieldOwner), client.ForceOwnership); err != nil {
		return nil, fmt.Errorf("applying adopting ClusterObjectSet: %w", err)
	}

	// Freshly created: completedAt is nil, so the caller gates the pipeline.
	return &orbv1alpha1.ClusterObjectSet{ObjectMeta: metav1.ObjectMeta{Name: name}}, nil
}

// desiredMatchesRevision reports whether the desired adopting COS content is
// already reflected in the given existing revision, comparing the (externalized)
// desired spec against the existing spec via DeepDerivative. The desired
// candidate is built with the existing revision's name and number so that
// name-derived slice references and the immutable revision field line up.
func (m *OrbStorageMigrator) desiredMatchesRevision(
	ctx context.Context,
	ext *ocv1.ClusterExtension,
	helmRelease *release.Release,
	objectLabels map[string]string,
	existing *orbv1alpha1.ClusterObjectSet,
) (bool, error) {
	desired, err := m.buildDesired(ctx, ext, helmRelease, objectLabels, existing.Name, existing.Spec.Revision)
	if err != nil {
		return false, err
	}
	externalized, _, err := orb.ExternalizeCOS(desired)
	if err != nil {
		return false, fmt.Errorf("externalizing adopting ClusterObjectSet for comparison: %w", err)
	}
	return specDeepDerivative(externalized, existing)
}

// buildDesired generates a fresh adopting COS apply configuration for the given
// release and stamps it with the given name, revision number, and a
// non-controller owner reference to the ClusterExtension. Regenerating for each
// use avoids aliasing the phases mutated in place by externalization.
func (m *OrbStorageMigrator) buildDesired(
	ctx context.Context,
	ext *ocv1.ClusterExtension,
	helmRelease *release.Release,
	objectLabels map[string]string,
	name string,
	revision uint32,
) (*orbac.ClusterObjectSetApplyConfiguration, error) {
	desired, err := m.RevisionGenerator.GenerateRevisionFromHelmRelease(ctx, helmRelease, ext, objectLabels)
	if err != nil {
		return nil, err
	}
	desired.WithName(name)
	desired.Spec.WithRevision(revision)

	gvk, err := apiutil.GVKForObject(ext, m.Scheme)
	if err != nil {
		return nil, fmt.Errorf("get GVK for owner: %w", err)
	}
	// Non-controller owner reference: orb's adoptOrphans only adopts a COS with
	// no controller owner, so this lets the COD later set itself as controller.
	// It still garbage-collects the revision with the ClusterExtension during
	// the pre-COD window.
	desired.WithOwnerReferences(metav1ac.OwnerReference().
		WithAPIVersion(gvk.GroupVersion().String()).
		WithKind(gvk.Kind).
		WithName(ext.Name).
		WithUID(ext.UID).
		WithBlockOwnerDeletion(true))
	return desired, nil
}

// deleteHelmReleaseStorage deletes the Helm bookkeeping secrets for the release
// oldest-to-newest (ascending version), so a partial-delete failure leaves the
// newest deployed release present and the next reconcile resumes against the
// same release rather than rewinding to an older one. This is not a Helm
// uninstall: the managed objects are now orb-adopted and must not be torn down.
func (m *OrbStorageMigrator) deleteHelmReleaseStorage(ac helmclient.ActionInterface, name string) error {
	cfg := ac.Config()
	if cfg == nil || cfg.Releases == nil {
		return fmt.Errorf("Helm release storage unavailable")
	}

	history, err := cfg.Releases.History(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	slices.SortFunc(history, func(a, b *release.Release) int {
		return cmp.Compare(a.Version, b.Version)
	})
	for _, rel := range history {
		if _, err := cfg.Releases.Delete(name, rel.Version); err != nil {
			return fmt.Errorf("deleting Helm release %q version %d storage: %w", name, rel.Version, err)
		}
	}
	return nil
}

func nextOrbRevisionNumber(existing []orbv1alpha1.ClusterObjectSet) uint32 {
	if len(existing) == 0 {
		return 1
	}
	return existing[len(existing)-1].Spec.Revision + 1
}

// specDeepDerivative reports whether every field set in the desired COS spec is
// already reflected in the existing COS spec (mirrors the applier's
// alreadyApplied gating), so an already-captured desired spec does not spuriously
// increment the revision number.
func specDeepDerivative(desired *orbac.ClusterObjectSetApplyConfiguration, existing *orbv1alpha1.ClusterObjectSet) (bool, error) {
	desiredSpec, err := specAsMap(desired)
	if err != nil {
		return false, fmt.Errorf("marshaling desired spec: %w", err)
	}
	existingSpec, err := specAsMap(existing)
	if err != nil {
		return false, fmt.Errorf("marshaling existing spec: %w", err)
	}
	return equality.Semantic.DeepDerivative(desiredSpec, existingSpec), nil
}

func specAsMap(obj any) (map[string]any, error) {
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	spec, _ := m["spec"].(map[string]any)
	return spec, nil
}
