package applier

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"helm.sh/helm/v3/pkg/release"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	helmstorage "github.com/operator-framework/helm-operator-plugins/pkg/storage"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	deploymentutil "github.com/operator-framework/operator-controller/internal/operator-controller/clusterobjectdeployment"
	"github.com/operator-framework/operator-controller/internal/operator-controller/revisions"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	hashutil "github.com/operator-framework/operator-controller/internal/shared/util/hash"
)

// BoxcutterStorageMigrator adopts stored Helm content through a deployment, then
// enables collision protection before allowing ordinary bundle resolution.
type BoxcutterStorageMigrator struct {
	ReleaseDriver     helmclient.ObjectToStorageDriverMapper
	RevisionGenerator RevisionGenerator
	Client            client.Client
	APIReader         client.Reader
	Scheme            *runtime.Scheme
	SystemNamespace   string
	FieldOwner        string
}

// PendingMigration holds the deployment for CE status reporting.
type PendingMigration struct {
	Deployment *ocv1.ClusterObjectDeployment
}

// Migrate gates ordinary reconciliation while Helm history remains. Once the COD
// exists, its persisted template is sufficient to advance the rollout.
func (m *BoxcutterStorageMigrator) Migrate(ctx context.Context, ext *ocv1.ClusterExtension, objectLabels map[string]string) (*PendingMigration, error) {
	if !ext.DeletionTimestamp.IsZero() {
		return nil, nil
	}
	indexes, err := m.listHelmIndexes(ctx, ext)
	if err != nil || len(indexes) == 0 {
		return nil, err
	}

	cod := &ocv1.ClusterObjectDeployment{}
	switch err := m.APIReader.Get(ctx, client.ObjectKey{Name: ext.Name}, cod); {
	case err == nil:
		return m.advance(ctx, ext, cod, indexes)
	case !apierrors.IsNotFound(err):
		return nil, fmt.Errorf("getting migration deployment: %w", err)
	}

	existing, err := revisions.ListForGroup(ctx, m.Client, ext.Name)
	if err != nil {
		return nil, fmt.Errorf("listing ClusterObjectSets before migration: %w", err)
	}
	if len(existing) != 0 {
		return nil, fmt.Errorf("cannot migrate Helm release %q while its ClusterObjectSet group already exists", ext.Name)
	}

	helmRelease, err := m.getDeployedRelease(ctx, ext, indexes)
	if err != nil {
		return nil, err
	}
	generated, secrets, err := m.RevisionGenerator.GenerateClusterObjectDeploymentFromHelmRelease(ctx, helmRelease, ext, objectLabels)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(generated)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cod); err != nil {
		return nil, err
	}
	if err := controllerutil.SetControllerReference(ext, cod, m.Scheme); err != nil {
		return nil, err
	}
	for i := range secrets {
		if err := m.storeContent(ctx, ext, &secrets[i]); err != nil {
			return nil, err
		}
	}
	// Create only: a concurrent deployment may belong to another controller.
	if err := m.Client.Create(ctx, cod, client.FieldOwner(m.FieldOwner)); err != nil {
		return nil, fmt.Errorf("creating migration deployment: %w", err)
	}
	return &PendingMigration{Deployment: cod}, nil
}

func (m *BoxcutterStorageMigrator) advance(ctx context.Context, ext *ocv1.ClusterExtension, cod *ocv1.ClusterObjectDeployment, indexes []corev1.Secret) (*PendingMigration, error) {
	if !cod.DeletionTimestamp.IsZero() {
		return nil, fmt.Errorf("migration deployment %q is deleting", cod.Name)
	}
	if !controlledBy(cod, ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind)) ||
		cod.Labels[labels.ManagedByKey] != labels.ManagedByOperatorController {
		return nil, fmt.Errorf("migration deployment %q is not managed by operator-controller for ClusterExtension UID %q", cod.Name, ext.UID)
	}
	protection := cod.Spec.Template.Spec.CollisionProtection
	if protection != ocv1.CollisionProtectionNone && protection != ocv1.CollisionProtectionPrevent {
		return nil, fmt.Errorf("migration deployment %q has unexpected collision protection %q", cod.Name, protection)
	}
	pending := &PendingMigration{Deployment: cod}
	if !deploymentutil.RolloutComplete(cod) {
		return pending, nil
	}

	if protection == ocv1.CollisionProtectionNone {
		if err := m.enableProtection(ctx, cod); err != nil {
			return nil, err
		}
		return pending, nil
	}
	observed, err := m.protectedRevisionObserved(ctx, cod)
	if err != nil {
		return nil, err
	}
	if observed {
		if err := m.deleteHelmHistory(ctx, indexes); err != nil {
			return nil, err
		}
	}
	// A subsequent reconcile confirms that cleanup is complete.
	return pending, nil
}

func (m *BoxcutterStorageMigrator) enableProtection(ctx context.Context, cod *ocv1.ClusterObjectDeployment) error {
	// Apply the persisted template, including every phase and assertion, with
	// the same field manager that the CE will use for subsequent bundle updates.
	spec := cod.Spec.DeepCopy()
	spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionPrevent
	data, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	var desired ocv1ac.ClusterObjectDeploymentSpecApplyConfiguration
	if err := json.Unmarshal(data, &desired); err != nil {
		return err
	}
	owner := metav1.GetControllerOf(cod)
	config := ocv1ac.ClusterObjectDeployment(cod.Name).
		WithResourceVersion(cod.ResourceVersion).
		WithUID(cod.UID).
		WithLabels(map[string]string{
			labels.ManagedByKey: labels.ManagedByOperatorController,
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: owner.Name,
		}).
		WithOwnerReferences(metav1ac.OwnerReference().
			WithAPIVersion(owner.APIVersion).
			WithKind(owner.Kind).
			WithName(owner.Name).
			WithUID(owner.UID).
			WithController(true).
			WithBlockOwnerDeletion(true)).
		WithSpec(&desired)
	// Create recorded the initial fields as an Update manager. Transfer them
	// to the Apply manager only for the version whose adoption was verified.
	if err := m.Client.Apply(ctx, config, client.FieldOwner(m.FieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("enabling collision protection: %w", err)
	}
	cod.Spec = *spec
	return nil
}

func (m *BoxcutterStorageMigrator) protectedRevisionObserved(ctx context.Context, cod *ocv1.ClusterObjectDeployment) (bool, error) {
	cached := &ocv1.ClusterObjectDeployment{}
	if err := m.Client.Get(ctx, client.ObjectKeyFromObject(cod), cached); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	if cached.UID != cod.UID || cached.Generation != cod.Generation {
		return false, nil
	}
	sets, err := revisions.ListForGroup(ctx, m.Client, cod.Name)
	if err != nil {
		return false, fmt.Errorf("checking protected ClusterObjectSet before Helm cleanup: %w", err)
	}
	hash := hashutil.DeepHashObject(cod.Spec.Template)
	for i := range sets {
		set := &sets[i]
		if metav1.IsControlledBy(set, cod) && set.Labels[labels.TemplateHashKey] == hash &&
			set.DeletionTimestamp.IsZero() && set.Spec.LifecycleState != ocv1.ClusterObjectSetLifecycleStateArchived &&
			!set.Status.CompletedAt.IsZero() {
			return true, nil
		}
	}
	return false, nil
}

func (m *BoxcutterStorageMigrator) storeContent(ctx context.Context, ext *ocv1.ClusterExtension, secret *corev1.Secret) error {
	if err := controllerutil.SetControllerReference(ext, secret, m.Scheme); err != nil {
		return err
	}
	if err := m.Client.Create(ctx, secret); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating migration content Secret %q: %w", secret.Name, err)
		}
		existing := &corev1.Secret{}
		if err := m.APIReader.Get(ctx, client.ObjectKeyFromObject(secret), existing); err != nil {
			return fmt.Errorf("reading migration content Secret %q: %w", secret.Name, err)
		}
		if !metav1.IsControlledBy(existing, ext) || !existing.DeletionTimestamp.IsZero() ||
			existing.Immutable == nil || !*existing.Immutable || existing.Type != secret.Type ||
			!reflect.DeepEqual(existing.Data, secret.Data) {
			return fmt.Errorf("existing migration content Secret %q does not match the extension's immutable content", secret.Name)
		}
	}
	return nil
}

// These labels are written by the chunked Helm storage driver.
const helmStorageOwner = "operator-controller"

func (m *BoxcutterStorageMigrator) listHelmIndexes(ctx context.Context, ext *ocv1.ClusterExtension) ([]corev1.Secret, error) {
	var secrets corev1.SecretList
	if err := m.APIReader.List(ctx, &secrets, client.InNamespace(m.SystemNamespace),
		client.MatchingLabels{"owner": helmStorageOwner, "type": "index", "name": ext.Name}); err != nil {
		return nil, fmt.Errorf("listing Helm release Secrets: %w", err)
	}
	var indexes []corev1.Secret
	for _, secret := range secrets.Items {
		if secret.Type == helmstorage.SecretTypeChunkedIndex && controlledBy(&secret, ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind)) {
			indexes = append(indexes, secret)
		}
	}
	return indexes, nil
}

func (m *BoxcutterStorageMigrator) getDeployedRelease(ctx context.Context, ext *ocv1.ClusterExtension, indexes []corev1.Secret) (*release.Release, error) {
	var latest *corev1.Secret
	var version int
	for i := range indexes {
		index := &indexes[i]
		if !index.DeletionTimestamp.IsZero() || index.Labels["status"] != release.StatusDeployed.String() {
			continue
		}
		revision, err := strconv.Atoi(index.Labels["version"])
		if err != nil || revision < 1 {
			return nil, fmt.Errorf("invalid Helm revision on Secret %q", index.Name)
		}
		if revision > version {
			latest, version = index, revision
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("Helm release %q has history but no deployed revision to migrate", ext.Name)
	}
	driver, err := m.ReleaseDriver(ctx, ext, nil)
	if err != nil {
		return nil, fmt.Errorf("opening Helm release storage: %w", err)
	}
	rel, err := driver.Get(latest.Name)
	if err != nil {
		return nil, fmt.Errorf("reading deployed Helm release from %q: %w", latest.Name, err)
	}
	if rel == nil || rel.Name != ext.Name || rel.Version != version || rel.Info == nil || rel.Info.Status != release.StatusDeployed {
		return nil, fmt.Errorf("Helm release in Secret %q does not match the selected deployed revision", latest.Name)
	}
	return rel, nil
}

func (m *BoxcutterStorageMigrator) deleteHelmHistory(ctx context.Context, indexes []corev1.Secret) error {
	for i := range indexes {
		index := &indexes[i]
		var chunks corev1.SecretList
		if err := m.APIReader.List(ctx, &chunks, client.InNamespace(index.Namespace),
			client.MatchingLabels{"owner": helmStorageOwner, "type": "chunk", "key": index.Name}); err != nil {
			return fmt.Errorf("listing Helm chunks for %q: %w", index.Name, err)
		}
		chunksRemain := false
		for j := range chunks.Items {
			chunk := &chunks.Items[j]
			if chunk.Type != helmstorage.SecretTypeChunkedChunk || !controlledBy(chunk, index, corev1.SchemeGroupVersion.WithKind("Secret")) {
				continue
			}
			chunksRemain = true
			if err := m.deleteHelmSecret(ctx, chunk); err != nil {
				return err
			}
		}
		// Keep the index as the cleanup checkpoint until the next list confirms
		// that all of its chunks were removed.
		if !chunksRemain {
			if err := m.deleteHelmSecret(ctx, index); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *BoxcutterStorageMigrator) deleteHelmSecret(ctx context.Context, secret *corev1.Secret) error {
	if err := client.IgnoreNotFound(m.Client.Delete(ctx, secret, client.Preconditions{UID: &secret.UID})); err != nil {
		return fmt.Errorf("deleting Helm Secret %q: %w", secret.Name, err)
	}
	return nil
}

func controlledBy(obj, owner client.Object, gvk schema.GroupVersionKind) bool {
	ref := metav1.GetControllerOf(obj)
	return ref != nil && ref.Name == owner.GetName() && ref.UID == owner.GetUID() &&
		ref.APIVersion == gvk.GroupVersion().String() && ref.Kind == gvk.Kind
}
