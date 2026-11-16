package applier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	deploymentutil "github.com/operator-framework/operator-controller/internal/operator-controller/clusterobjectdeployment"
	"github.com/operator-framework/operator-controller/internal/operator-controller/revisions"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

// BoxcutterStorageMigrator adopts stored Helm content through a deployment, then
// enables collision protection before allowing ordinary bundle resolution.
type BoxcutterStorageMigrator struct {
	ActionClientGetter helmclient.ActionClientGetter
	RevisionGenerator  RevisionGenerator
	Client             client.Client
	Scheme             *runtime.Scheme
}

// Migrate returns a deployment while migration is pending; nil means skipped or
// complete. Once created, its template and immutable Secrets are the only inputs
// to migration. Retries and restarts never require Helm, a catalog or a registry.
func (m *BoxcutterStorageMigrator) Migrate(ctx context.Context, ext *ocv1.ClusterExtension, objectLabels map[string]string) (*ocv1.ClusterObjectDeployment, error) {
	if !ext.DeletionTimestamp.IsZero() {
		return nil, nil
	}
	cod := &ocv1.ClusterObjectDeployment{}
	switch err := m.Client.Get(ctx, client.ObjectKey{Name: ext.Name}, cod); {
	case err == nil:
		if _, marked := cod.Annotations[labels.HelmMigrationKey]; !marked {
			return nil, nil
		}
		return m.advance(ctx, ext, cod)
	case !apierrors.IsNotFound(err):
		return nil, fmt.Errorf("getting migration deployment: %w", err)
	}

	// An omitted namespace identifies a new-style installation that never used Helm.
	if ext.Spec.Namespace == "" {
		return nil, nil
	}
	existing, err := revisions.ListForGroup(ctx, m.Client, ext.Name)
	if err != nil {
		return nil, fmt.Errorf("listing ClusterObjectSets before migration: %w", err)
	}
	if len(existing) != 0 {
		// Existing experimental layouts do not authorize reconstructing a deployment.
		return nil, nil
	}
	ac, err := m.ActionClientGetter.ActionClientFor(ctx, ext)
	if err != nil {
		return nil, err
	}
	helmRelease, err := ac.Get(ext.Name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if helmRelease == nil || helmRelease.Info == nil || helmRelease.Info.Status != release.StatusDeployed {
		helmRelease, err = m.findLatestDeployedRelease(ac, ext.Name)
		if err != nil || helmRelease == nil {
			return nil, err
		}
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
	if cod.Annotations == nil {
		cod.Annotations = map[string]string{}
	}
	cod.Annotations[labels.HelmMigrationKey] = labels.HelmMigrationAdopting
	for i := range secrets {
		if err := m.storeContent(ctx, ext, &secrets[i]); err != nil {
			return nil, err
		}
	}
	// Create only: a concurrently created deployment must never be overwritten.
	// AlreadyExists is retried through the read/validation path above.
	if err := m.Client.Create(ctx, cod); err != nil {
		return nil, fmt.Errorf("creating migration deployment: %w", err)
	}
	return cod, nil
}

func (m *BoxcutterStorageMigrator) advance(ctx context.Context, ext *ocv1.ClusterExtension, cod *ocv1.ClusterObjectDeployment) (*ocv1.ClusterObjectDeployment, error) {
	if !metav1.IsControlledBy(cod, ext) || !cod.DeletionTimestamp.IsZero() {
		return nil, fmt.Errorf("migration deployment %q must be non-deleting and controlled by ClusterExtension UID %q", cod.Name, ext.UID)
	}
	stage := cod.Annotations[labels.HelmMigrationKey]
	protection := cod.Spec.Template.Spec.CollisionProtection
	if (stage != labels.HelmMigrationAdopting || protection != ocv1.CollisionProtectionNone) &&
		(stage != labels.HelmMigrationProtecting || protection != ocv1.CollisionProtectionPrevent) {
		return nil, fmt.Errorf("migration deployment %q has inconsistent stage %q and collision protection %q", cod.Name, stage, protection)
	}
	if !deploymentutil.RolloutComplete(cod) {
		return cod, nil
	}
	before := cod.DeepCopy()
	if stage == labels.HelmMigrationAdopting {
		cod.Spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionPrevent
		cod.Annotations[labels.HelmMigrationKey] = labels.HelmMigrationProtecting
	} else {
		delete(cod.Annotations, labels.HelmMigrationKey)
	}
	if err := m.Client.Patch(ctx, cod, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		return nil, fmt.Errorf("advancing %s migration: %w", stage, err)
	}
	if stage == labels.HelmMigrationProtecting {
		return nil, nil
	}
	return cod, nil
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
		if err := m.Client.Get(ctx, client.ObjectKeyFromObject(secret), existing); err != nil {
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

// findLatestDeployedRelease preserves selection of the newest deployed release
// when the latest Helm revision is failed, pending or missing deployment status.
func (m *BoxcutterStorageMigrator) findLatestDeployedRelease(ac helmclient.ActionInterface, name string) (*release.Release, error) {
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
