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

	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/cli-runtime/pkg/printers"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authorization"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/shared/util/cache"
)

const (
	ClusterExtensionRevisionRetentionLimit = 5
)

type ClusterExtensionRevisionGenerator interface {
	GenerateRevision(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1.ClusterExtensionRevision, error)
	GenerateRevisionFromHelmRelease(
		ctx context.Context,
		helmRelease *release.Release, ext *ocv1.ClusterExtension,
		objectLabels map[string]string,
	) (*ocv1.ClusterExtensionRevision, error)
}

type SimpleRevisionGenerator struct {
	Scheme           *runtime.Scheme
	ManifestProvider ManifestProvider
}

func (r *SimpleRevisionGenerator) GenerateRevisionFromHelmRelease(
	ctx context.Context,
	helmRelease *release.Release, ext *ocv1.ClusterExtension,
	objectLabels map[string]string,
) (*ocv1.ClusterExtensionRevision, error) {
	docs := splitManifestDocuments(helmRelease.Manifest)
	objs := make([]ocv1.ClusterExtensionRevisionObject, 0, len(docs))
	for _, doc := range docs {
		obj := unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			return nil, err
		}
		obj.SetLabels(mergeLabelMaps(obj.GetLabels(), objectLabels))

		// Memory optimization: strip large annotations
		// Note: ApplyStripTransform never returns an error in practice
		_ = cache.ApplyStripAnnotationsTransform(&obj)
		sanitizedUnstructured(ctx, &obj)

		objs = append(objs, ocv1.ClusterExtensionRevisionObject{
			Object:              obj,
			CollisionProtection: ocv1.CollisionProtectionNone, // allow to adopt objects from previous release
		})
	}

	rev := r.buildClusterExtensionRevision(objs, ext, map[string]string{
		labels.BundleNameKey:      helmRelease.Labels[labels.BundleNameKey],
		labels.PackageNameKey:     helmRelease.Labels[labels.PackageNameKey],
		labels.BundleVersionKey:   helmRelease.Labels[labels.BundleVersionKey],
		labels.BundleReferenceKey: helmRelease.Labels[labels.BundleReferenceKey],
	})
	rev.Name = fmt.Sprintf("%s-1", ext.Name)
	rev.Spec.Revision = 1
	return rev, nil
}

func (r *SimpleRevisionGenerator) GenerateRevision(
	ctx context.Context,
	bundleFS fs.FS, ext *ocv1.ClusterExtension,
	objectLabels, revisionAnnotations map[string]string,
) (*ocv1.ClusterExtensionRevision, error) {
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
	objs := make([]ocv1.ClusterExtensionRevisionObject, 0, len(plain))
	for _, obj := range plain {
		obj.SetLabels(mergeLabelMaps(obj.GetLabels(), objectLabels))

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

		objs = append(objs, ocv1.ClusterExtensionRevisionObject{
			Object: unstr,
		})
	}
	return r.buildClusterExtensionRevision(objs, ext, revisionAnnotations), nil
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
	objects []ocv1.ClusterExtensionRevisionObject,
	ext *ocv1.ClusterExtension,
	annotations map[string]string,
) *ocv1.ClusterExtensionRevision {
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[labels.ServiceAccountNameKey] = ext.Spec.ServiceAccount.Name
	annotations[labels.ServiceAccountNamespaceKey] = ext.Spec.Namespace

	cer := &ocv1.ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: annotations,
			Labels: map[string]string{
				labels.OwnerKindKey: ocv1.ClusterExtensionKind,
				labels.OwnerNameKey: ext.Name,
			},
		},
		Spec: ocv1.ClusterExtensionRevisionSpec{
			// Explicitly set LifecycleState to Active. While the CRD has a default,
			// being explicit here ensures all code paths are clear and doesn't rely
			// on API server defaulting behavior.
			LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateActive,
			Phases:         PhaseSort(objects),
		},
	}
	if p := ext.Spec.ProgressDeadlineMinutes; p > 0 {
		cer.Spec.ProgressDeadlineMinutes = p
	}
	return cer
}

// BoxcutterStorageMigrator migrates ClusterExtensions from Helm-based storage to
// ClusterExtensionRevision storage, enabling upgrades from older operator-controller versions.
type BoxcutterStorageMigrator struct {
	ActionClientGetter helmclient.ActionClientGetter
	RevisionGenerator  ClusterExtensionRevisionGenerator
	Client             boxcutterStorageMigratorClient
	Scheme             *runtime.Scheme
}

type boxcutterStorageMigratorClient interface {
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
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
	if rev.Labels == nil {
		rev.Labels = make(map[string]string)
	}
	rev.Labels[labels.MigratedFromHelmKey] = "true"

	// Set ownerReference for proper garbage collection when the ClusterExtension is deleted.
	if err := controllerutil.SetControllerReference(ext, rev, m.Scheme); err != nil {
		return fmt.Errorf("set ownerref: %w", err)
	}

	if err := m.Client.Create(ctx, rev); err != nil {
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
	return m.ensureRevisionStatus(ctx, rev)
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
		return m.ensureRevisionStatus(ctx, &revisions[i])
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
func (m *BoxcutterStorageMigrator) ensureRevisionStatus(ctx context.Context, rev *ocv1.ClusterExtensionRevision) error {
	// Re-fetch to get latest version before checking status
	if err := m.Client.Get(ctx, client.ObjectKeyFromObject(rev), rev); err != nil {
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
}

// createOrUpdate creates or updates the revision object. PreAuthorization checks are performed to ensure the
// user has sufficient permissions to manage the revision and its resources
func (bc *Boxcutter) createOrUpdate(ctx context.Context, user user.Info, rev *ocv1.ClusterExtensionRevision) error {
	if rev.GetObjectKind().GroupVersionKind().Empty() {
		gvk, err := apiutil.GVKForObject(rev, bc.Scheme)
		if err != nil {
			return err
		}
		rev.GetObjectKind().SetGroupVersionKind(gvk)
	}

	// Run auth preflight checks
	if err := bc.runPreAuthorizationChecks(ctx, user, rev); err != nil {
		return err
	}

	return bc.Client.Patch(ctx, rev, client.Apply, client.FieldOwner(bc.FieldOwner), client.ForceOwnership)
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

	if err := controllerutil.SetControllerReference(ext, desiredRevision, bc.Scheme); err != nil {
		return false, "", fmt.Errorf("set ownerref: %w", err)
	}

	currentRevision := &ocv1.ClusterExtensionRevision{}
	state := StateNeedsInstall
	// check if we can update the current revision.
	if len(existingRevisions) > 0 {
		// try first to update the current revision.
		currentRevision = &existingRevisions[len(existingRevisions)-1]
		desiredRevision.Spec.Revision = currentRevision.Spec.Revision
		desiredRevision.Name = currentRevision.Name

		err := bc.createOrUpdate(ctx, getUserInfo(ext), desiredRevision)
		switch {
		case apierrors.IsInvalid(err):
			// We could not update the current revision due to trying to update an immutable field.
			// Therefore, we need to create a new revision.
			state = StateNeedsUpgrade
		case err == nil:
			// inplace patch was successful, no changes in phases
			state = StateUnchanged
		default:
			return false, "", fmt.Errorf("patching %s Revision: %w", desiredRevision.Name, err)
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
		// need to create new revision
		prevRevisions := existingRevisions
		revisionNumber := latestRevisionNumber(prevRevisions) + 1

		desiredRevision.Name = fmt.Sprintf("%s-%d", ext.Name, revisionNumber)
		desiredRevision.Spec.Revision = revisionNumber

		if err = bc.garbageCollectOldRevisions(ctx, prevRevisions); err != nil {
			return false, "", fmt.Errorf("garbage collecting old revisions: %w", err)
		}

		if err := bc.createOrUpdate(ctx, getUserInfo(ext), desiredRevision); err != nil {
			return false, "", fmt.Errorf("creating new Revision: %w", err)
		}
	}

	return true, "", nil
}

// runPreAuthorizationChecks runs PreAuthorization checks if the PreAuthorizer is set. An error will be returned if
// the ClusterExtension service account does not have the necessary permissions to manage the revision's resources
func (bc *Boxcutter) runPreAuthorizationChecks(ctx context.Context, user user.Info, rev *ocv1.ClusterExtensionRevision) error {
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
func getObjects(rev *ocv1.ClusterExtensionRevision) []client.Object {
	totalObjects := 0
	for _, phase := range rev.Spec.Phases {
		totalObjects += len(phase.Objects)
	}
	objs := make([]client.Object, 0, totalObjects)
	for _, phase := range rev.Spec.Phases {
		for _, phaseObject := range phase.Objects {
			objs = append(objs, &phaseObject.Object)
		}
	}
	return objs
}

// revisionManifestReader returns an io.Reader containing all manifests in the revision
func revisionManifestReader(rev *ocv1.ClusterExtensionRevision) (io.Reader, error) {
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

func revisionManagementPerms(rev *ocv1.ClusterExtensionRevision) func(user.Info) []authorizer.AttributesRecord {
	return func(user user.Info) []authorizer.AttributesRecord {
		return []authorizer.AttributesRecord{
			{
				User:            user,
				Name:            rev.Name,
				APIGroup:        ocv1.GroupVersion.Group,
				APIVersion:      ocv1.GroupVersion.Version,
				Resource:        "clusterextensionrevisions/finalizers",
				ResourceRequest: true,
				Verb:            "update",
			},
		}
	}
}

func mergeLabelMaps(m1, m2 map[string]string) map[string]string {
	mergedLabels := make(map[string]string, len(m1)+len(m2))
	maps.Copy(mergedLabels, m1)
	maps.Copy(mergedLabels, m2)
	return mergedLabels
}
