package applier

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"sort"
	"strings"

	"github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/revisions"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	"github.com/operator-framework/operator-controller/internal/shared/util/cache"
	hashutil "github.com/operator-framework/operator-controller/internal/shared/util/hash"
)

// contentHashAnnotation records a fingerprint of the rendered content on the
// ClusterObjectDeployment. The applier uses it to detect when the content is
// unchanged between reconciles and skip redundant preflights. It lives on the
// ClusterObjectDeployment's top-level metadata (not its template), so it does not
// affect the template hash the ClusterObjectDeployment controller uses to cut
// revisions.
const contentHashAnnotation = "olm.operatorframework.io/content-hash"

// RevisionGenerator renders bundle content into the revision-bearing artifacts
// the applier applies: a ClusterObjectDeployment (the primary path) or, for the
// Helm-based migration path, a ClusterObjectSet revision directly.
//
// TODO: reconsider this name (e.g. ClusterObjectDeploymentGenerator) once the
// Helm-based ClusterExtension -> boxcutter-based ClusterExtension migration work
// removes or relocates GenerateRevisionFromHelmRelease, so this interface no
// longer also produces a ClusterObjectSet.
type RevisionGenerator interface {
	GenerateClusterObjectDeployment(
		ctx context.Context,
		bundleFS fs.FS, ext *ocv1.ClusterExtension,
		objectLabels, revisionAnnotations map[string]string,
	) (*ocv1ac.ClusterObjectDeploymentApplyConfiguration, []corev1.Secret, error)
	GenerateRevisionFromHelmRelease(
		ctx context.Context,
		helmRelease *release.Release, ext *ocv1.ClusterExtension,
		objectLabels map[string]string,
	) (*ocv1ac.ClusterObjectSetApplyConfiguration, error)
}

type SimpleRevisionGenerator struct {
	Scheme           *runtime.Scheme
	ManifestProvider ManifestProvider
	SystemNamespace  string
}

func (r *SimpleRevisionGenerator) GenerateRevisionFromHelmRelease(
	ctx context.Context,
	helmRelease *release.Release, ext *ocv1.ClusterExtension,
	objectLabels map[string]string,
) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
	docs := splitManifestDocuments(helmRelease.Manifest)
	objs := make([]ocv1ac.ClusterObjectSetObjectApplyConfiguration, 0, len(docs))
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
		if v, ok := helmRelease.Labels[labels.BundleReleaseKey]; ok {
			annotationUpdates[labels.BundleReleaseKey] = v
		}
		if v := helmRelease.Labels[labels.PackageNameKey]; v != "" {
			annotationUpdates[labels.PackageNameKey] = v
		}
		if len(annotationUpdates) > 0 {
			obj.SetAnnotations(mergeStringMaps(obj.GetAnnotations(), annotationUpdates))
		}

		objs = append(objs, *ocv1ac.ClusterObjectSetObject().
			WithObject(obj))
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
	rev := r.buildClusterObjectSet(objs, ext, revisionAnnotations)
	rev.WithName(fmt.Sprintf("%s-1", ext.Name))
	rev.Spec.WithRevision(1)
	rev.Spec.WithCollisionProtection(ocv1.CollisionProtectionNone) // allow to adopt objects from previous release
	return rev, nil
}

// renderObjects converts plain client.Objects to ClusterObjectSetObjectApplyConfigurations
// with labels and annotations applied. This is shared logic used by both COS and COD generation.
func (r *SimpleRevisionGenerator) renderObjects(
	ctx context.Context,
	plain []client.Object,
	objectLabels, objectAnnotations map[string]string,
) ([]ocv1ac.ClusterObjectSetObjectApplyConfiguration, error) {
	objs := make([]ocv1ac.ClusterObjectSetObjectApplyConfiguration, 0, len(plain))
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

		if len(objectAnnotations) > 0 {
			unstr.SetAnnotations(mergeStringMaps(unstr.GetAnnotations(), objectAnnotations))
		}

		objs = append(objs, *ocv1ac.ClusterObjectSetObject().
			WithObject(unstr))
	}
	return objs, nil
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

func (r *SimpleRevisionGenerator) buildClusterObjectSet(
	objects []ocv1ac.ClusterObjectSetObjectApplyConfiguration,
	ext *ocv1.ClusterExtension,
	annotations map[string]string,
) *ocv1ac.ClusterObjectSetApplyConfiguration {
	if annotations == nil {
		annotations = make(map[string]string)
	}

	phases := PhaseSort(objects)

	spec := ocv1ac.ClusterObjectSetSpec().
		WithGroup(ext.Name).
		WithLifecycleState(ocv1.ClusterObjectSetLifecycleStateActive).
		WithPhases(phases...).
		WithProgressionProbes(defaultProgressionProbes...)

	return ocv1ac.ClusterObjectSet("").
		WithAnnotations(annotations).
		WithLabels(map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.Name,
		}).
		WithSpec(spec)
}

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

// TODO(COD): Replace direct COS creation, revision discovery and retention with
// ClusterObjectDeployment reconciliation.
type Boxcutter struct {
	Client            client.Client
	Scheme            *runtime.Scheme
	RevisionGenerator RevisionGenerator
	Preflights        []Preflight
	FieldOwner        string
	SystemNamespace   string
}

func (bc *Boxcutter) Apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (bool, string, error) {
	// If contentFS is nil, we're maintaining the current state without catalog access.
	// In this case, we rely on the already-installed ClusterObjectDeployment (and its
	// ClusterObjectDeployment controller) to keep reconciling the workload.
	//
	// TODO: a nil contentFS is an indirect signal for "resolved bundle content is
	// unavailable". It conflates "catalog access" into the applier, which has no knowledge
	// of the source type used to resolve the bundle. This maintain-current-state decision
	// should move to a higher layer and key off a more direct signal than contentFS==nil.
	if contentFS == nil {
		existing := &ocv1.ClusterObjectDeployment{}
		err := bc.Client.Get(ctx, client.ObjectKey{Name: ext.Name}, existing)
		switch {
		case err == nil:
			// Maintain current state: the ClusterObjectDeployment controller keeps
			// reconciling the workload even without catalog access. The returned bool is
			// advisory here — ClusterExtension status is derived from the
			// ClusterObjectDeployment's rollout conditions, not from this value.
			return true, "", nil
		case apierrors.IsNotFound(err):
			return false, "", fmt.Errorf("catalog content unavailable and no deployment installed")
		default:
			return false, "", fmt.Errorf("getting ClusterObjectDeployment %q: %w", ext.Name, err)
		}
	}

	// Generate the desired ClusterObjectDeployment and the Secrets its template refs.
	cod, secrets, err := bc.RevisionGenerator.GenerateClusterObjectDeployment(ctx, contentFS, ext, objectLabels, revisionAnnotations)
	if err != nil {
		return false, "", err
	}

	gvk, err := apiutil.GVKForObject(ext, bc.Scheme)
	if err != nil {
		return false, "", fmt.Errorf("get GVK for owner: %w", err)
	}
	cod.WithOwnerReferences(metav1ac.OwnerReference().
		WithAPIVersion(gvk.GroupVersion().String()).
		WithKind(gvk.Kind).
		WithName(ext.Name).
		WithUID(ext.UID).
		WithBlockOwnerDeletion(true).
		WithController(true))

	// Fingerprint the rendered content so a later reconcile can tell whether anything
	// actually changed. Packed Secret names are content-addressed, so their sorted set
	// uniquely identifies the content. Stamp it on the ClusterObjectDeployment's top-level
	// metadata (not its template) and compare against the already-applied value below.
	secretNames := make([]string, len(secrets))
	for i := range secrets {
		secretNames[i] = secrets[i].Name
	}
	sort.Strings(secretNames)
	contentHash := hashutil.DeepHashObject(secretNames)
	cod.WithAnnotations(map[string]string{contentHashAnnotation: contentHash})

	if err := bc.runPreflights(ctx, ext, contentHash, secrets); err != nil {
		return false, "", err
	}

	// Create the referenced Secrets before applying the COD so the referenced content
	// always exists for the ClusterObjectDeployment controller. Secrets are
	// content-addressed and immutable, so AlreadyExists is expected and tolerated.
	for i := range secrets {
		if err := controllerutil.SetControllerReference(ext, &secrets[i], bc.Scheme); err != nil {
			return false, "", fmt.Errorf("setting owner reference on ref Secret %q: %w", secrets[i].Name, err)
		}
		if err := bc.Client.Create(ctx, &secrets[i]); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return false, "", fmt.Errorf("creating ref Secret %q: %w", secrets[i].Name, err)
			}
		}
	}

	// Server-side apply the ClusterObjectDeployment. The ClusterObjectDeployment
	// controller owns revision (ClusterObjectSet) creation, rollout and retention.
	if err := bc.Client.Apply(ctx, cod, client.FieldOwner(bc.FieldOwner), client.ForceOwnership); err != nil {
		return false, "", fmt.Errorf("applying ClusterObjectDeployment: %w", err)
	}

	return true, "", nil
}

// runPreflights runs the configured preflight checks for a desired apply. It determines whether
// this is an install or an upgrade from whether the ClusterObjectDeployment already exists (a
// direct, controller-owned signal, rather than our own ClusterExtension status which may be stale
// or modified externally), and short-circuits — skipping the preflights entirely — when the
// already-applied deployment carries a matching content hash, so the CRD-safety preflights do not
// re-run on every steady-state reconcile.
func (bc *Boxcutter) runPreflights(ctx context.Context, ext *ocv1.ClusterExtension, contentHash string, secrets []corev1.Secret) error {
	state := StateNeedsInstall
	existing := &ocv1.ClusterObjectDeployment{}
	switch getErr := bc.Client.Get(ctx, client.ObjectKey{Name: ext.Name}, existing); {
	case getErr == nil:
		if existing.Annotations[contentHashAnnotation] == contentHash {
			state = StateUnchanged
		} else {
			state = StateNeedsUpgrade
		}
	case !apierrors.IsNotFound(getErr):
		return fmt.Errorf("getting ClusterObjectDeployment %q: %w", ext.Name, getErr)
	}
	if state == StateUnchanged {
		return nil
	}

	// Decode the packed objects only when preflights will actually run.
	plainObjs, err := objectsFromSecrets(secrets)
	if err != nil {
		return fmt.Errorf("decoding packed objects for preflights: %w", err)
	}
	for _, preflight := range bc.Preflights {
		if shouldSkipPreflight(ctx, preflight, ext, state) {
			continue
		}
		switch state {
		case StateNeedsInstall:
			if err := preflight.Install(ctx, plainObjs); err != nil {
				return err
			}
		case StateNeedsUpgrade:
			if err := preflight.Upgrade(ctx, plainObjs); err != nil {
				return err
			}
		}
	}
	return nil
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

func mergeStringMaps(m1, m2 map[string]string) map[string]string {
	merged := make(map[string]string, len(m1)+len(m2))
	maps.Copy(merged, m1)
	maps.Copy(merged, m2)
	return merged
}

// extractPhasesForPacking converts apply configuration phases to API types for SecretPacker.
func extractPhasesForPacking(phases []ocv1ac.ClusterObjectSetPhaseApplyConfiguration) []ocv1.ClusterObjectSetPhase {
	result := make([]ocv1.ClusterObjectSetPhase, 0, len(phases))
	for _, p := range phases {
		phase := ocv1.ClusterObjectSetPhase{}
		if p.Name != nil {
			phase.Name = *p.Name
		}
		phase.Objects = make([]ocv1.ClusterObjectSetObject, 0, len(p.Objects))
		for _, o := range p.Objects {
			obj := ocv1.ClusterObjectSetObject{}
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
func replaceInlineWithRefs(rev *ocv1ac.ClusterObjectSetApplyConfiguration, pack *PackResult) {
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
