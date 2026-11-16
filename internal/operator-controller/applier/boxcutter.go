package applier

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"sort"
	"strings"

	"github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	"helm.sh/helm/v3/pkg/release"
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

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
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

// RevisionGenerator renders bundle or stored Helm content into a deployment and its content Secrets.
type RevisionGenerator interface {
	GenerateClusterObjectDeployment(
		ctx context.Context,
		bundleFS fs.FS, ext *ocv1.ClusterExtension,
		objectLabels, revisionAnnotations map[string]string,
	) (*ocv1ac.ClusterObjectDeploymentApplyConfiguration, []corev1.Secret, error)
	GenerateClusterObjectDeploymentFromHelmRelease(
		ctx context.Context,
		helmRelease *release.Release, ext *ocv1.ClusterExtension,
		objectLabels map[string]string,
	) (*ocv1ac.ClusterObjectDeploymentApplyConfiguration, []corev1.Secret, error)
}

type SimpleRevisionGenerator struct {
	Scheme           *runtime.Scheme
	ManifestProvider ManifestProvider
	SystemNamespace  string
}

// renderHelmReleaseContent converts stored Helm manifests and bundle metadata
// into sanitized inline objects and revision annotations.
func renderHelmReleaseContent(
	ctx context.Context,
	helmRelease *release.Release,
	objectLabels map[string]string,
) ([]ocv1ac.ClusterObjectSetObjectApplyConfiguration, map[string]string, error) {
	docs := splitManifestDocuments(helmRelease.Manifest)
	objs := make([]ocv1ac.ClusterObjectSetObjectApplyConfiguration, 0, len(docs))
	for _, doc := range docs {
		obj := unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			return nil, nil, err
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
			WithType(ocv1.ObjectTypeInlineManifest).
			WithInlineManifest(obj))
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
	return objs, revisionAnnotations, nil
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
			WithType(ocv1.ObjectTypeInlineManifest).
			WithInlineManifest(unstr))
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

	// Refuse foreign-controlled deployments and ownerless deployments without our
	// managed-by label. Returning an ordinary error lets the ClusterExtension report
	// Progressing=True/Retrying and use the normal controller retry backoff.
	if err := bc.checkAdoptable(ctx, ext); err != nil {
		return false, "", err
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

// checkAdoptable verifies that the applier may own the ClusterObjectDeployment named
// after the ClusterExtension. Ownerless deployments must have the operator-controller
// managed-by label. A deployment controlled by this extension's UID is always adoptable.
// Other controller UIDs are refused regardless of labels. If a restore leaves a
// stale owner reference, the deployment can be recreated after garbage collection.
func (bc *Boxcutter) checkAdoptable(ctx context.Context, ext *ocv1.ClusterExtension) error {
	existing := &ocv1.ClusterObjectDeployment{}
	switch err := bc.Client.Get(ctx, client.ObjectKey{Name: ext.Name}, existing); {
	case apierrors.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("getting ClusterObjectDeployment %q: %w", ext.Name, err)
	}

	controller := metav1.GetControllerOf(existing)
	if controller == nil {
		if existing.Labels[labels.ManagedByKey] == labels.ManagedByOperatorController {
			return nil
		}
		return fmt.Errorf(
			"ClusterObjectDeployment %q has no controller and does not have the required %s=%s label; refusing to adopt it",
			ext.Name, labels.ManagedByKey, labels.ManagedByOperatorController,
		)
	}
	if controller.UID == ext.UID {
		return nil
	}

	return fmt.Errorf(
		"ClusterObjectDeployment %q is already controlled by %s %q; refusing to adopt it",
		ext.Name, controller.Kind, controller.Name,
	)
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
			if o.Type != nil {
				obj.Type = *o.Type
			}
			if o.InlineManifest != nil {
				obj.InlineManifest = *o.InlineManifest
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
			rev.Spec.Phases[phaseIdx].Objects[objIdx] = *ocv1ac.ClusterObjectSetObject().
				WithType(ocv1.ObjectTypeSecretReference).
				WithSecretRef(ocv1ac.SecretSourceRef().
					WithName(ref.Name).
					WithNamespace(ref.Namespace).
					WithKey(ref.Key))
		}
	}
}
