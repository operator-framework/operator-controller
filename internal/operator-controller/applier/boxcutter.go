package applier

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/cli-runtime/pkg/printers"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authorization"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/shared/util/cache"
)

const (
	ClusterExtensionRevisionRetentionLimit = 5
)

type ClusterExtensionRevisionGenerator interface {
	GenerateRevision(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1ac.ClusterExtensionRevisionApplyConfiguration, error)
	GenerateRevisionFromHelmRelease(
		ctx context.Context,
		helmRelease *release.Release, ext *ocv1.ClusterExtension,
		objectLabels map[string]string,
	) (*ocv1ac.ClusterExtensionRevisionApplyConfiguration, error)
}

type SimpleRevisionGenerator struct {
	Scheme           *runtime.Scheme
	ManifestProvider ManifestProvider
}

func (r *SimpleRevisionGenerator) GenerateRevisionFromHelmRelease(
	ctx context.Context,
	helmRelease *release.Release, ext *ocv1.ClusterExtension,
	objectLabels map[string]string,
) (*ocv1ac.ClusterExtensionRevisionApplyConfiguration, error) {
	docs := splitManifestDocuments(helmRelease.Manifest)
	objs := make([]ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration, 0, len(docs))
	for _, doc := range docs {
		obj := unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			return nil, err
		}
		obj.SetLabels(mergeStringMaps(obj.GetLabels(), objectLabels))

		// Memory optimization: strip large annotations
		// Note: ApplyStripTransform never returns an error in practice
		_ = cache.ApplyStripAnnotationsTransform(&obj)
		sanitizedUnstructured(ctx, &obj)

		annotationUpdates := map[string]string{}
		if v := helmRelease.Labels[labels.BundleVersionKey]; v != "" {
			annotationUpdates[labels.BundleVersionKey] = v
		}
		if v := helmRelease.Labels[labels.PackageNameKey]; v != "" {
			annotationUpdates[labels.PackageNameKey] = v
		}
		if len(annotationUpdates) > 0 {
			obj.SetAnnotations(mergeStringMaps(obj.GetAnnotations(), annotationUpdates))
		}

		objs = append(objs, *ocv1ac.ClusterExtensionRevisionObject().
			WithObject(obj))
	}

	rev := r.buildClusterExtensionRevision(objs, ext, map[string]string{
		labels.BundleNameKey:      helmRelease.Labels[labels.BundleNameKey],
		labels.PackageNameKey:     helmRelease.Labels[labels.PackageNameKey],
		labels.BundleVersionKey:   helmRelease.Labels[labels.BundleVersionKey],
		labels.BundleReferenceKey: helmRelease.Labels[labels.BundleReferenceKey],
	})
	rev.WithName(fmt.Sprintf("%s-1", ext.Name))
	rev.Spec.WithRevision(1)
	rev.Spec.WithCollisionProtection(ocv1.CollisionProtectionNone) // allow to adopt objects from previous release
	return rev, nil
}

func (r *SimpleRevisionGenerator) GenerateRevision(
	ctx context.Context,
	bundleFS fs.FS, ext *ocv1.ClusterExtension,
	objectLabels, revisionAnnotations map[string]string,
) (*ocv1ac.ClusterExtensionRevisionApplyConfiguration, error) {
	// extract plain manifests
	plain, err := r.ManifestProvider.Get(bundleFS, ext)
	if err != nil {
		return nil, err
	}

	if revisionAnnotations == nil {
		revisionAnnotations = map[string]string{}
	}

	// add bundle properties of interest to revision annotations
	bundleAnnotations, err := getBundleAnnotations(bundleFS)
	if err != nil {
		return nil, fmt.Errorf("error getting bundle annotations: %w", err)
	}

	// we don't care about all of the bundle and csv annotations as they can be quite confusing
	// e.g. 'createdAt', 'capabilities', etc.
	for _, key := range []string{
		// used by other operators that care about the bundle properties (e.g. maxOpenShiftVersion)
		source.PropertyOLMProperties,
	} {
		if value, ok := bundleAnnotations[key]; ok {
			revisionAnnotations[key] = value
		}
	}

	// objectLabels
	objs := make([]ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration, 0, len(plain))
	for _, obj := range plain {
		obj.SetLabels(mergeStringMaps(obj.GetLabels(), objectLabels))

		gvk, err := apiutil.GVKForObject(obj, r.Scheme)
		if err != nil {
			return nil, err
		}

		unstrObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return nil, err
		}
		unstr := unstructured.Unstructured{Object: unstrObj}
		unstr.SetGroupVersionKind(gvk)

		// Memory optimization: strip large annotations
		if err := cache.ApplyStripAnnotationsTransform(&unstr); err != nil {
			return nil, err
		}
		sanitizedUnstructured(ctx, &unstr)

		annotationUpdates := map[string]string{}
		if v := revisionAnnotations[labels.BundleVersionKey]; v != "" {
			annotationUpdates[labels.BundleVersionKey] = v
		}
		if v := revisionAnnotations[labels.PackageNameKey]; v != "" {
			annotationUpdates[labels.PackageNameKey] = v
		}
		if len(annotationUpdates) > 0 {
			unstr.SetAnnotations(mergeStringMaps(unstr.GetAnnotations(), annotationUpdates))
		}

		objs = append(objs, *ocv1ac.ClusterExtensionRevisionObject().
			WithObject(unstr))
	}
	rev := r.buildClusterExtensionRevision(objs, ext, revisionAnnotations)
	rev.Spec.WithCollisionProtection(ocv1.CollisionProtectionPrevent)
	return rev, nil
}

// sanitizedUnstructured takes an unstructured obj, removes status if present, and returns a sanitized copy containing only the allowed metadata entries set below.
// If any unallowed entries are removed, a warning will be logged.
func sanitizedUnstructured(ctx context.Context, unstr *unstructured.Unstructured) {
	l := log.FromContext(ctx)
	obj := unstr.Object

	// remove status
	if _, ok := obj["status"]; ok {
		l.Info("warning: extraneous status removed from manifest")
		delete(obj, "status")
	}

	var allowedMetadata = []string{
		"annotations",
		"labels",
		"name",
		"namespace",
	}

	var metadata map[string]any
	if metaRaw, ok := obj["metadata"]; ok {
		metadata, ok = metaRaw.(map[string]any)
		if !ok {
			return
		}
	} else {
		return
	}

	metadataSanitized := map[string]any{}
	for _, key := range allowedMetadata {
		if val, ok := metadata[key]; ok {
			metadataSanitized[key] = val
		}
	}

	if len(metadataSanitized) != len(metadata) {
		l.Info("warning: extraneous values removed from manifest metadata", "allowed metadata", allowedMetadata)
	}
	obj["metadata"] = metadataSanitized
}

func (r *SimpleRevisionGenerator) buildClusterExtensionRevision(
	objects []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration,
	ext *ocv1.ClusterExtension,
	annotations map[string]string,
) *ocv1ac.ClusterExtensionRevisionApplyConfiguration {
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[labels.ServiceAccountNameKey] = ext.Spec.ServiceAccount.Name
	annotations[labels.ServiceAccountNamespaceKey] = ext.Spec.Namespace

	phases := PhaseSort(objects)

	spec := ocv1ac.ClusterExtensionRevisionSpec().
		WithLifecycleState(ocv1.ClusterExtensionRevisionLifecycleStateActive).
		WithPhases(phases...).
		WithProgressionProbes(defaultProgressionProbes...)
	if p := ext.Spec.ProgressDeadlineMinutes; p > 0 {
		spec.WithProgressDeadlineMinutes(p)
	}

	return ocv1ac.ClusterExtensionRevision("").
		WithAnnotations(annotations).
		WithLabels(map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.Name,
		}).
		WithSpec(spec)
}

// BoxcutterStorageMigrator migrates ClusterExtensions from Helm-based storage to
// ClusterExtensionRevision storage, enabling upgrades from older operator-controller versions.
type BoxcutterStorageMigrator struct {
	ActionClientGetter helmclient.ActionClientGetter
	RevisionGenerator  ClusterExtensionRevisionGenerator
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

// Migrate creates a ClusterExtensionRevision from an existing Helm release if no revisions exist yet.
// The migration is idempotent and skipped if revisions already exist or no Helm release is found.
func (m *BoxcutterStorageMigrator) Migrate(ctx context.Context, ext *ocv1.ClusterExtension, objectLabels map[string]string) error {
	existingRevisionList := ocv1.ClusterExtensionRevisionList{}
	if err := m.Client.List(ctx, &existingRevisionList, client.MatchingLabels{
		labels.OwnerNameKey: ext.Name,
	}); err != nil {
		return fmt.Errorf("listing ClusterExtensionRevisions before attempting migration: %w", err)
	}
	if len(existingRevisionList.Items) != 0 {
		return m.ensureMigratedRevisionStatus(ctx, existingRevisionList.Items)
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
	// While the ClusterExtensionRevision controller would eventually reconcile and set this status,
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
func (m *BoxcutterStorageMigrator) ensureMigratedRevisionStatus(ctx context.Context, revisions []ocv1.ClusterExtensionRevision) error {
	for i := range revisions {
		if revisions[i].Spec.Revision != 1 {
			continue
		}
		// Skip if already succeeded - status is already set correctly.
		if meta.IsStatusConditionTrue(revisions[i].Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded) {
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

// ensureRevisionStatus ensures the revision has the Succeeded status condition set.
// Returns nil if the status is already set or after successfully setting it.
// Only sets status on revisions that were actually migrated from Helm (marked with MigratedFromHelmKey label).
func (m *BoxcutterStorageMigrator) ensureRevisionStatus(ctx context.Context, name string) error {
	rev := &ocv1.ClusterExtensionRevision{}
	if err := m.Client.Get(ctx, client.ObjectKey{Name: name}, rev); err != nil {
		return fmt.Errorf("getting existing revision for status check: %w", err)
	}

	// Only set status if this revision was actually migrated from Helm.
	// This prevents us from incorrectly marking normal Boxcutter revision 1 as succeeded
	// when it's still in progress.
	if rev.Labels[labels.MigratedFromHelmKey] != "true" {
		return nil
	}

	// Check if status is already set to Succeeded=True
	if meta.IsStatusConditionTrue(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded) {
		return nil
	}

	// Set the Succeeded status condition
	meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterExtensionRevisionTypeSucceeded,
		Status:             metav1.ConditionTrue,
		Reason:             ocv1.ReasonSucceeded,
		Message:            "Revision succeeded - migrated from Helm release",
		ObservedGeneration: rev.GetGeneration(),
	})

	if err := m.Client.Status().Update(ctx, rev); err != nil {
		return fmt.Errorf("updating migrated revision status: %w", err)
	}

	return nil
}

type Boxcutter struct {
	Client            client.Client
	Scheme            *runtime.Scheme
	RevisionGenerator ClusterExtensionRevisionGenerator
	Preflights        []Preflight
	PreAuthorizer     authorization.PreAuthorizer
	FieldOwner        string
	SystemNamespace   string
}

func (bc *Boxcutter) Apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (bool, string, error) {
	// List existing revisions first to validate cluster connectivity before checking contentFS.
	// This ensures we fail fast on API errors rather than attempting fallback behavior when
	// cluster access is unavailable (since the ClusterExtensionRevision controller also requires
	// API access to maintain resources). The revision list is also needed to determine if fallback
	// is possible when contentFS is nil (at least one revision must exist).
	existingRevisions, err := bc.getExistingRevisions(ctx, ext.GetName())
	if err != nil {
		return false, "", err
	}

	// If contentFS is nil, we're maintaining the current state without catalog access.
	// In this case, we should use the existing installed revision without generating a new one.
	if contentFS == nil {
		if len(existingRevisions) == 0 {
			return false, "", fmt.Errorf("catalog content unavailable and no revision installed")
		}
		// Returning true here signals that the rollout has succeeded using the current revision.
		// This assumes the ClusterExtensionRevision controller is running and will continue to
		// reconcile, apply, and maintain the resources defined in that revision via Server-Side Apply,
		// ensuring the workload keeps running even when catalog access is unavailable.
		return true, "", nil
	}

	// Generate desired revision
	desiredRevision, err := bc.RevisionGenerator.GenerateRevision(ctx, contentFS, ext, objectLabels, revisionAnnotations)
	if err != nil {
		return false, "", err
	}

	gvk, err := apiutil.GVKForObject(ext, bc.Scheme)
	if err != nil {
		return false, "", fmt.Errorf("get GVK for owner: %w", err)
	}
	desiredRevision.WithOwnerReferences(metav1ac.OwnerReference().
		WithAPIVersion(gvk.GroupVersion().String()).
		WithKind(gvk.Kind).
		WithName(ext.Name).
		WithUID(ext.UID).
		WithBlockOwnerDeletion(true).
		WithController(true))

	currentRevision := &ocv1.ClusterExtensionRevision{}
	state := StateNeedsInstall
	if len(existingRevisions) > 0 {
		currentRevision = &existingRevisions[len(existingRevisions)-1]
		desiredRevision.Spec.WithRevision(currentRevision.Spec.Revision)
		desiredRevision.WithName(currentRevision.Name)

		// Save inline objects before externalization (needed for preflights + createExternalizedRevision)
		savedInline := saveInlineObjects(desiredRevision)

		// Externalize with CURRENT revision name so refs match existing CER
		phases := extractPhasesForPacking(desiredRevision.Spec.Phases)
		packer := &SecretPacker{
			RevisionName:    currentRevision.Name,
			OwnerName:       ext.Name,
			SystemNamespace: bc.SystemNamespace,
		}
		packResult, err := packer.Pack(phases)
		if err != nil {
			return false, "", fmt.Errorf("packing for SSA comparison: %w", err)
		}
		replaceInlineWithRefs(desiredRevision, packResult)

		// SSA patch (refs-vs-refs). Skip pre-auth — just checking for changes.
		// createExternalizedRevision runs its own pre-auth if upgrade is needed.
		err = bc.Client.Apply(ctx, desiredRevision, client.FieldOwner(bc.FieldOwner), client.ForceOwnership)

		// Restore inline objects for preflights + createExternalizedRevision
		restoreInlineObjects(desiredRevision, savedInline)

		switch {
		case apierrors.IsInvalid(err):
			state = StateNeedsUpgrade
		case err == nil:
			state = StateUnchanged
		default:
			return false, "", fmt.Errorf("patching %s Revision: %w", currentRevision.Name, err)
		}
	}

	// Preflights
	plainObjs := getObjects(desiredRevision)
	for _, preflight := range bc.Preflights {
		if shouldSkipPreflight(ctx, preflight, ext, state) {
			continue
		}
		switch state {
		case StateNeedsInstall:
			err := preflight.Install(ctx, plainObjs)
			if err != nil {
				return false, "", err
			}
		// TODO: jlanford's IDE says that "StateNeedsUpgrade" condition is always true, but
		//   it isn't immediately obvious why that is. Perhaps len(existingRevisions) is
		//   always greater than 0 (seems unlikely), or shouldSkipPreflight always returns
		//   true (and we continue) when state == StateNeedsInstall?
		case StateNeedsUpgrade:
			err := preflight.Upgrade(ctx, plainObjs)
			if err != nil {
				return false, "", err
			}
		}
	}

	if state != StateUnchanged {
		if err := bc.createExternalizedRevision(ctx, ext, desiredRevision, existingRevisions); err != nil {
			return false, "", err
		}
	} else if currentRevision.Name != "" {
		// In-place patch succeeded. Ensure any existing ref Secrets have ownerReferences
		// (crash recovery for Step 3 failures).
		if err := bc.ensureSecretOwnerReferences(ctx, currentRevision); err != nil {
			return false, "", fmt.Errorf("ensuring ownerReferences on ref Secrets: %w", err)
		}
	}

	return true, "", nil
}

// createExternalizedRevision creates a new CER with all objects externalized to Secrets.
// It follows a crash-safe three-step sequence: create Secrets, create CER, patch ownerRefs.
func (bc *Boxcutter) createExternalizedRevision(ctx context.Context, ext *ocv1.ClusterExtension, desiredRevision *ocv1ac.ClusterExtensionRevisionApplyConfiguration, existingRevisions []ocv1.ClusterExtensionRevision) error {
	prevRevisions := existingRevisions
	revisionNumber := latestRevisionNumber(prevRevisions) + 1

	revisionName := fmt.Sprintf("%s-%d", ext.Name, revisionNumber)
	desiredRevision.WithName(revisionName)
	desiredRevision.Spec.WithRevision(revisionNumber)

	if err := bc.garbageCollectOldRevisions(ctx, prevRevisions); err != nil {
		return fmt.Errorf("garbage collecting old revisions: %w", err)
	}

	// Run pre-authorization on the inline revision (before replacing objects with refs)
	if err := bc.runPreAuthorizationChecks(ctx, getUserInfo(ext), desiredRevision); err != nil {
		return fmt.Errorf("creating new Revision: %w", err)
	}

	// Externalize: pack inline objects into Secrets and replace with refs
	phases := extractPhasesForPacking(desiredRevision.Spec.Phases)
	packer := &SecretPacker{
		RevisionName:    revisionName,
		OwnerName:       ext.Name,
		SystemNamespace: bc.SystemNamespace,
	}
	packResult, err := packer.Pack(phases)
	if err != nil {
		return fmt.Errorf("packing objects into Secrets: %w", err)
	}
	replaceInlineWithRefs(desiredRevision, packResult)

	// Step 1: Create Secrets (skip AlreadyExists)
	for i := range packResult.Secrets {
		if err := bc.Client.Create(ctx, &packResult.Secrets[i]); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("creating ref Secret %q: %w", packResult.Secrets[i].Name, err)
			}
		}
	}

	// Step 2: Create CER with refs via SSA (pre-auth already ran above)
	if err := bc.Client.Apply(ctx, desiredRevision, client.FieldOwner(bc.FieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("creating new Revision: %w", err)
	}

	// Step 3: Patch ownerReferences onto Secrets using the CER's UID
	if err := bc.patchSecretOwnerReferences(ctx, desiredRevision, packResult.Secrets); err != nil {
		return fmt.Errorf("patching ownerReferences on ref Secrets: %w", err)
	}
	return nil
}

// runPreAuthorizationChecks runs PreAuthorization checks if the PreAuthorizer is set. An error will be returned if
// the ClusterExtension service account does not have the necessary permissions to manage the revision's resources
func (bc *Boxcutter) runPreAuthorizationChecks(ctx context.Context, user user.Info, rev *ocv1ac.ClusterExtensionRevisionApplyConfiguration) error {
	if bc.PreAuthorizer == nil {
		return nil
	}

	// collect the revision manifests
	manifestReader, err := revisionManifestReader(rev)
	if err != nil {
		return err
	}

	// run preauthorization check
	return formatPreAuthorizerOutput(bc.PreAuthorizer.PreAuthorize(ctx, user, manifestReader, revisionManagementPerms(rev)))
}

// garbageCollectOldRevisions deletes archived revisions beyond ClusterExtensionRevisionRetentionLimit.
// Active revisions are never deleted. revisionList must be sorted oldest to newest.
func (bc *Boxcutter) garbageCollectOldRevisions(ctx context.Context, revisionList []ocv1.ClusterExtensionRevision) error {
	for index, r := range revisionList {
		// Only delete archived revisions that are beyond the limit
		if index < len(revisionList)-ClusterExtensionRevisionRetentionLimit && r.Spec.LifecycleState == ocv1.ClusterExtensionRevisionLifecycleStateArchived {
			if err := bc.Client.Delete(ctx, &ocv1.ClusterExtensionRevision{
				ObjectMeta: metav1.ObjectMeta{
					Name: r.Name,
				},
			}); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("deleting archived revision: %w", err)
			}
		}
	}
	return nil
}

// getExistingRevisions returns the list of ClusterExtensionRevisions for a ClusterExtension with name extName in revision order (oldest to newest)
func (bc *Boxcutter) getExistingRevisions(ctx context.Context, extName string) ([]ocv1.ClusterExtensionRevision, error) {
	existingRevisionList := &ocv1.ClusterExtensionRevisionList{}
	if err := bc.Client.List(ctx, existingRevisionList, client.MatchingLabels{
		labels.OwnerNameKey: extName,
	}); err != nil {
		return nil, fmt.Errorf("listing revisions: %w", err)
	}
	slices.SortFunc(existingRevisionList.Items, func(a, b ocv1.ClusterExtensionRevision) int {
		return cmp.Compare(a.Spec.Revision, b.Spec.Revision)
	})
	return existingRevisionList.Items, nil
}

func latestRevisionNumber(prevRevisions []ocv1.ClusterExtensionRevision) int64 {
	if len(prevRevisions) == 0 {
		return 0
	}
	return prevRevisions[len(prevRevisions)-1].Spec.Revision
}

var (
	// defaultProgressionProbes is the default set of progression probes used to check for phase readiness
	defaultProgressionProbes = []*ocv1ac.ProgressionProbeApplyConfiguration{
		// CRD probe
		ocv1ac.ProgressionProbe().
			WithSelector(ocv1ac.ObjectSelector().
				WithType(ocv1.SelectorTypeGroupKind).
				WithGroupKind(metav1.GroupKind{
					Group: "apiextensions.k8s.io",
					Kind:  "CustomResourceDefinition",
				})).
			WithAssertions(ocv1ac.Assertion().
				WithType(ocv1.ProbeTypeConditionEqual).
				WithConditionEqual(
					ocv1ac.ConditionEqualProbe().
						WithType(string(apiextensions.Established)).
						WithStatus(string(corev1.ConditionTrue)))),
		// certmanager Certificate probe
		ocv1ac.ProgressionProbe().
			WithSelector(ocv1ac.ObjectSelector().
				WithType(ocv1.SelectorTypeGroupKind).
				WithGroupKind(metav1.GroupKind{
					Group: certmanager.GroupName,
					Kind:  "Certificate",
				})).
			WithAssertions(readyConditionAssertion),
		// certmanager Issuer probe
		ocv1ac.ProgressionProbe().
			WithSelector(ocv1ac.ObjectSelector().
				WithType(ocv1.SelectorTypeGroupKind).
				WithGroupKind(metav1.GroupKind{
					Group: certmanager.GroupName,
					Kind:  "Issuer",
				})).
			WithAssertions(readyConditionAssertion),
		// namespace probe; asserts that the namespace is in "Active" phase
		ocv1ac.ProgressionProbe().
			WithSelector(ocv1ac.ObjectSelector().
				WithType(ocv1.SelectorTypeGroupKind).
				WithGroupKind(metav1.GroupKind{
					Group: corev1.GroupName,
					Kind:  "Namespace",
				})).
			WithAssertions(ocv1ac.Assertion().
				WithType(ocv1.ProbeTypeFieldValue).
				WithFieldValue(ocv1ac.FieldValueProbe().
					WithFieldPath("status.phase").
					WithValue(string(corev1.NamespaceActive)))),
		// PVC probe; asserts that the PVC is in "Bound" phase
		ocv1ac.ProgressionProbe().
			WithSelector(ocv1ac.ObjectSelector().
				WithType(ocv1.SelectorTypeGroupKind).
				WithGroupKind(metav1.GroupKind{
					Group: corev1.GroupName,
					Kind:  "PersistentVolumeClaim",
				})).
			WithAssertions(ocv1ac.Assertion().
				WithType(ocv1.ProbeTypeFieldValue).
				WithFieldValue(ocv1ac.FieldValueProbe().
					WithFieldPath("status.phase").
					WithValue(string(corev1.ClaimBound)))),
		// StatefulSet probe
		ocv1ac.ProgressionProbe().WithSelector(
			ocv1ac.ObjectSelector().WithType(ocv1.SelectorTypeGroupKind).
				WithGroupKind(metav1.GroupKind{
					Group: appsv1.GroupName,
					Kind:  "StatefulSet",
				}),
		).WithAssertions(replicasUpdatedAssertion, availableConditionAssertion),
		// Deployment probe
		ocv1ac.ProgressionProbe().WithSelector(
			ocv1ac.ObjectSelector().WithType(ocv1.SelectorTypeGroupKind).
				WithGroupKind(metav1.GroupKind{
					Group: appsv1.GroupName,
					Kind:  "Deployment",
				}),
		).WithAssertions(replicasUpdatedAssertion, availableConditionAssertion),
	}

	// readyConditionAssertion checks that the Type: "Ready" Condition is "True"
	readyConditionAssertion = ocv1ac.Assertion().
				WithType(ocv1.ProbeTypeConditionEqual).
				WithConditionEqual(
			ocv1ac.ConditionEqualProbe().
				WithType("Ready").
				WithStatus("True"))

	// availableConditionAssertion checks if the Type: "Available" Condition is "True".
	availableConditionAssertion = ocv1ac.Assertion().
					WithType(ocv1.ProbeTypeConditionEqual).
					WithConditionEqual(ocv1ac.ConditionEqualProbe().
						WithType(string(appsv1.DeploymentAvailable)).
						WithStatus(string(corev1.ConditionTrue)))

	// replicasUpdatedAssertion checks if status.updatedReplicas == status.replicas.
	// Works for StatefulSets, Deployments and ReplicaSets.
	replicasUpdatedAssertion = ocv1ac.Assertion().
					WithType(ocv1.ProbeTypeFieldsEqual).
					WithFieldsEqual(ocv1ac.FieldsEqualProbe().
						WithFieldA("status.updatedReplicas").
						WithFieldB("status.replicas"))
)

func splitManifestDocuments(file string) []string {
	// Estimate: typical manifests have ~50-100 lines per document
	// Pre-allocate for reasonable bundle size to reduce allocations
	lines := strings.Split(file, "\n")
	estimatedDocs := len(lines) / 20 // conservative estimate
	if estimatedDocs < 4 {
		estimatedDocs = 4
	}
	docs := make([]string, 0, estimatedDocs)

	for _, manifest := range lines {
		manifest = strings.TrimSpace(manifest)
		if len(manifest) == 0 {
			continue
		}
		docs = append(docs, manifest)
	}
	return docs
}

// getObjects returns a slice of all objects in the revision
func getObjects(rev *ocv1ac.ClusterExtensionRevisionApplyConfiguration) []client.Object {
	if rev.Spec == nil {
		return nil
	}
	totalObjects := 0
	for _, phase := range rev.Spec.Phases {
		totalObjects += len(phase.Objects)
	}
	objs := make([]client.Object, 0, totalObjects)
	for _, phase := range rev.Spec.Phases {
		for i := range phase.Objects {
			if phase.Objects[i].Object != nil {
				objs = append(objs, phase.Objects[i].Object)
			}
		}
	}
	return objs
}

// revisionManifestReader returns an io.Reader containing all manifests in the revision
func revisionManifestReader(rev *ocv1ac.ClusterExtensionRevisionApplyConfiguration) (io.Reader, error) {
	printer := printers.YAMLPrinter{}
	buf := new(bytes.Buffer)
	for _, obj := range getObjects(rev) {
		buf.WriteString("---\n")
		if err := printer.PrintObj(obj, buf); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

func revisionManagementPerms(rev *ocv1ac.ClusterExtensionRevisionApplyConfiguration) func(user.Info) []authorizer.AttributesRecord {
	return func(user user.Info) []authorizer.AttributesRecord {
		return []authorizer.AttributesRecord{
			{
				User:            user,
				Name:            *rev.GetName(),
				APIGroup:        ocv1.GroupVersion.Group,
				APIVersion:      ocv1.GroupVersion.Version,
				Resource:        "clusterextensionrevisions/finalizers",
				ResourceRequest: true,
				Verb:            "update",
			},
		}
	}
}

func mergeStringMaps(m1, m2 map[string]string) map[string]string {
	merged := make(map[string]string, len(m1)+len(m2))
	maps.Copy(merged, m1)
	maps.Copy(merged, m2)
	return merged
}

// saveInlineObjects saves Object pointers from each phase/object position.
func saveInlineObjects(rev *ocv1ac.ClusterExtensionRevisionApplyConfiguration) [][]*unstructured.Unstructured {
	saved := make([][]*unstructured.Unstructured, len(rev.Spec.Phases))
	for i, p := range rev.Spec.Phases {
		saved[i] = make([]*unstructured.Unstructured, len(p.Objects))
		for j, o := range p.Objects {
			saved[i][j] = o.Object
		}
	}
	return saved
}

// restoreInlineObjects restores saved inline objects and clears refs.
func restoreInlineObjects(rev *ocv1ac.ClusterExtensionRevisionApplyConfiguration, saved [][]*unstructured.Unstructured) {
	for i := range saved {
		for j := range saved[i] {
			rev.Spec.Phases[i].Objects[j].Object = saved[i][j]
			rev.Spec.Phases[i].Objects[j].Ref = nil
		}
	}
}

// extractPhasesForPacking converts apply configuration phases to API types for SecretPacker.
func extractPhasesForPacking(phases []ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration) []ocv1.ClusterExtensionRevisionPhase {
	result := make([]ocv1.ClusterExtensionRevisionPhase, 0, len(phases))
	for _, p := range phases {
		phase := ocv1.ClusterExtensionRevisionPhase{}
		if p.Name != nil {
			phase.Name = *p.Name
		}
		phase.Objects = make([]ocv1.ClusterExtensionRevisionObject, 0, len(p.Objects))
		for _, o := range p.Objects {
			obj := ocv1.ClusterExtensionRevisionObject{}
			if o.Object != nil {
				obj.Object = *o.Object
			}
			if o.CollisionProtection != nil {
				obj.CollisionProtection = *o.CollisionProtection
			}
			phase.Objects = append(phase.Objects, obj)
		}
		result = append(result, phase)
	}
	return result
}

// replaceInlineWithRefs replaces inline objects in the apply configuration with refs from the pack result.
func replaceInlineWithRefs(rev *ocv1ac.ClusterExtensionRevisionApplyConfiguration, pack *PackResult) {
	if rev.Spec == nil {
		return
	}
	for phaseIdx := range rev.Spec.Phases {
		for objIdx := range rev.Spec.Phases[phaseIdx].Objects {
			ref, ok := pack.Refs[[2]int{phaseIdx, objIdx}]
			if !ok {
				continue
			}
			rev.Spec.Phases[phaseIdx].Objects[objIdx].Object = nil
			rev.Spec.Phases[phaseIdx].Objects[objIdx].Ref = ocv1ac.ObjectSourceRef().
				WithName(ref.Name).
				WithNamespace(ref.Namespace).
				WithKey(ref.Key)
		}
	}
}

// patchSecretOwnerReferences fetches the CER to get its UID, then patches ownerReferences onto all Secrets.
func (bc *Boxcutter) patchSecretOwnerReferences(ctx context.Context, rev *ocv1ac.ClusterExtensionRevisionApplyConfiguration, secrets []corev1.Secret) error {
	if len(secrets) == 0 {
		return nil
	}

	// Fetch the CER to get its UID
	cer := &ocv1.ClusterExtensionRevision{}
	if err := bc.Client.Get(ctx, client.ObjectKey{Name: *rev.GetName()}, cer); err != nil {
		return fmt.Errorf("getting CER %q for ownerReference: %w", *rev.GetName(), err)
	}

	return bc.patchOwnerRefsOnSecrets(ctx, cer.Name, cer.UID, secrets)
}

// ensureSecretOwnerReferences checks referenced Secrets on an existing CER and patches missing ownerReferences.
// This handles crash recovery when Step 3 (patching ownerRefs) failed on a previous reconciliation.
func (bc *Boxcutter) ensureSecretOwnerReferences(ctx context.Context, cer *ocv1.ClusterExtensionRevision) error {
	// List Secrets with the revision-name label
	secretList := &corev1.SecretList{}
	if err := bc.Client.List(ctx, secretList,
		client.InNamespace(bc.SystemNamespace),
		client.MatchingLabels{labels.RevisionNameKey: cer.Name},
	); err != nil {
		return fmt.Errorf("listing ref Secrets for revision %q: %w", cer.Name, err)
	}

	var needsPatch []corev1.Secret
	for _, s := range secretList.Items {
		hasOwnerRef := false
		for _, ref := range s.OwnerReferences {
			if ref.UID == cer.UID {
				hasOwnerRef = true
				break
			}
		}
		if !hasOwnerRef {
			needsPatch = append(needsPatch, s)
		}
	}

	if len(needsPatch) == 0 {
		return nil
	}

	return bc.patchOwnerRefsOnSecrets(ctx, cer.Name, cer.UID, needsPatch)
}

// patchOwnerRefsOnSecrets patches ownerReferences onto the given Secrets, pointing to the CER.
func (bc *Boxcutter) patchOwnerRefsOnSecrets(ctx context.Context, cerName string, cerUID types.UID, secrets []corev1.Secret) error {
	ownerRef := metav1.OwnerReference{
		APIVersion:         ocv1.GroupVersion.String(),
		Kind:               ocv1.ClusterExtensionRevisionKind,
		Name:               cerName,
		UID:                cerUID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}

	for i := range secrets {
		s := &secrets[i]
		// Check if ownerRef already set
		alreadySet := false
		for _, ref := range s.OwnerReferences {
			if ref.UID == cerUID {
				alreadySet = true
				break
			}
		}
		if alreadySet {
			continue
		}

		patch := client.MergeFrom(s.DeepCopy())
		s.OwnerReferences = append(s.OwnerReferences, ownerRef)
		if err := bc.Client.Patch(ctx, s, patch); err != nil {
			return fmt.Errorf("patching ownerReference on Secret %s/%s: %w", s.Namespace, s.Name, err)
		}
	}
	return nil
}
