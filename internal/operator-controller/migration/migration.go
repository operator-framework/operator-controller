package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

const (
	fieldManager = "olm.operatorframework.io/migration"
)

// annotationPrefixesToStrip are annotation prefixes that should be removed from migrated resources.
var annotationPrefixesToStrip = []string{
	"kubectl.kubernetes.io/",
	"olm.operatorframework.io/installed-alongside",
	"deployment.kubernetes.io/",
}

// Backup holds serialized copies of resources for recovery.
type Backup struct {
	Subscription          *operatorsv1alpha1.Subscription
	ClusterServiceVersion *operatorsv1alpha1.ClusterServiceVersion
	Dir                   string // directory where backup files are stored
}

// SaveToDisk writes the backup resources as YAML files to a directory.
// The directory is created under the given base path as <base>/olm-migration-backup-<subscription>-<timestamp>.
func (b *Backup) SaveToDisk(basePath string) error {
	timestamp := time.Now().Format("20060102-150405")
	b.Dir = filepath.Join(basePath, fmt.Sprintf("olm-migration-backup-%s-%s", b.Subscription.Name, timestamp))

	if err := os.MkdirAll(b.Dir, 0o750); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	if err := writeYAML(filepath.Join(b.Dir, "subscription.yaml"), b.Subscription); err != nil {
		return fmt.Errorf("failed to write Subscription backup: %w", err)
	}

	if err := writeYAML(filepath.Join(b.Dir, "clusterserviceversion.yaml"), b.ClusterServiceVersion); err != nil {
		return fmt.Errorf("failed to write CSV backup: %w", err)
	}

	return nil
}

func writeYAML(path string, obj interface{}) error {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Migrate performs the full migration of an OLMv0-managed operator to OLMv1.
// The steps follow the RFC ordering:
//  1. Profile the Operator
//  2. Determine Compatibility and Readiness
//  3. Determine Target ClusterCatalog
//  4. Prepare the Operator for Migration (backup + delete Sub/CSV)
//  5. Collect Operator Resources
//  6. Create ClusterExtensionRevision
//  7. Create ClusterExtension
//  8. Clean Up
func (m *Migrator) Migrate(ctx context.Context, opts Options) error {
	opts.ApplyDefaults()

	_, csv, ip, err := m.GetCSVAndInstallPlan(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to profile operator: %w", err)
	}

	info, err := m.GetBundleInfo(ctx, opts, csv, ip)
	if err != nil {
		return fmt.Errorf("failed to get bundle info: %w", err)
	}

	readiness, err := m.CheckReadiness(ctx, opts)
	if err != nil {
		return fmt.Errorf("readiness check failed: %w", err)
	}
	if !readiness.Passed() {
		return fmt.Errorf("readiness checks failed (%d issues)", len(readiness.FailedChecks()))
	}

	propsJSON := csv.Annotations["operatorframework.io/properties"]
	compat, err := m.CheckCompatibility(ctx, opts, csv, propsJSON)
	if err != nil {
		return fmt.Errorf("compatibility check failed: %w", err)
	}
	if !compat.Passed() {
		return fmt.Errorf("operator is not compatible with OLMv1 migration (%d issues found)", len(compat.FailedChecks()))
	}

	catalogName, err := m.ResolveClusterCatalog(ctx, info, m.RESTConfig)
	if err != nil {
		return fmt.Errorf("failed to resolve ClusterCatalog: %w", err)
	}
	info.ResolvedCatalogName = catalogName

	backup, err := m.BackupResources(ctx, opts, csv)
	if err != nil {
		return fmt.Errorf("failed to backup resources: %w", err)
	}
	if err := m.PrepareForMigration(ctx, opts, csv); err != nil {
		if recoverErr := m.RecoverFromBackup(ctx, opts, backup); recoverErr != nil {
			return fmt.Errorf("preparation failed: %w; recovery also failed: %v", err, recoverErr)
		}
		return fmt.Errorf("preparation failed (recovered): %w", err)
	}

	objects, err := m.CollectResources(ctx, opts, csv, ip, info.PackageName)
	if err != nil {
		return fmt.Errorf("failed to collect resources: %w", err)
	}
	info.CollectedObjects = objects

	if err := m.CreateClusterExtensionRevision(ctx, opts, info); err != nil {
		if recoverErr := m.RecoverBeforeCE(ctx, opts, backup); recoverErr != nil {
			return fmt.Errorf("CER creation failed: %w; recovery also failed: %v", err, recoverErr)
		}
		return fmt.Errorf("CER creation failed (recovered): %w", err)
	}

	if err := m.CreateClusterExtension(ctx, opts, info); err != nil {
		return fmt.Errorf("failed to create ClusterExtension: %w", err)
	}

	m.CleanupOLMv0Resources(ctx, opts, info.PackageName, csv.Name)

	return nil
}

// EnsurePrerequisites verifies that all prerequisites for migration are met.
func (m *Migrator) EnsurePrerequisites(ctx context.Context, opts Options) (*operatorsv1alpha1.ClusterServiceVersion, *operatorsv1alpha1.InstallPlan, *PreMigrationReport, *PreMigrationReport, error) {
	readiness, err := m.CheckReadiness(ctx, opts)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	_, csv, ip, err := m.GetCSVAndInstallPlan(ctx, opts)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	propsJSON := csv.Annotations["operatorframework.io/properties"]
	compat, err := m.CheckCompatibility(ctx, opts, csv, propsJSON)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	return csv, ip, readiness, compat, nil
}

// BackupResources creates backup copies of the Subscription and CSV for recovery.
func (m *Migrator) BackupResources(ctx context.Context, opts Options, csv *operatorsv1alpha1.ClusterServiceVersion) (*Backup, error) {
	var sub operatorsv1alpha1.Subscription
	if err := m.Client.Get(ctx, types.NamespacedName{
		Name:      opts.SubscriptionName,
		Namespace: opts.SubscriptionNamespace,
	}, &sub); err != nil {
		return nil, fmt.Errorf("failed to backup Subscription: %w", err)
	}

	return &Backup{
		Subscription:          sub.DeepCopy(),
		ClusterServiceVersion: csv.DeepCopy(),
	}, nil
}

// PrepareForMigration shields the operator from OLMv0 reconciliation (RFC Step 4).
//  1. Ensure the installer ServiceAccount exists with cluster-admin binding
//  2. Delete the Subscription (prevents OLMv0 from creating new CSVs)
//  3. Delete the CSV with orphan cascading (detaches from owned resources)
func (m *Migrator) PrepareForMigration(ctx context.Context, opts Options, csv *operatorsv1alpha1.ClusterServiceVersion) error {
	// Ensure installer ServiceAccount with cluster-admin binding
	if err := m.EnsureInstallerServiceAccount(ctx, opts); err != nil {
		return fmt.Errorf("failed to ensure installer ServiceAccount: %w", err)
	}

	// Delete Subscription with orphan cascading
	sub := &operatorsv1alpha1.Subscription{}
	sub.Name = opts.SubscriptionName
	sub.Namespace = opts.SubscriptionNamespace
	if err := m.Client.Delete(ctx, sub, client.PropagationPolicy(metav1.DeletePropagationOrphan)); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete Subscription: %w", err)
		}
	}

	// Delete CSV with orphan cascading
	if err := m.Client.Delete(ctx, csv, client.PropagationPolicy(metav1.DeletePropagationOrphan)); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete CSV: %w", err)
		}
	}

	return nil
}

// EnsureInstallerServiceAccount creates the install namespace (if needed), the installer
// ServiceAccount, and binds it to cluster-admin. OLMv1 requires a ServiceAccount with
// cluster-admin privileges for the ClusterExtensionRevision to reconcile.
//
// If the install namespace differs from the subscription namespace, Pod Security Admission
// labels are copied from the subscription namespace to ensure the same security policy applies.
func (m *Migrator) EnsureInstallerServiceAccount(ctx context.Context, opts Options) error {
	// Copy PSA labels from subscription namespace if install namespace differs
	psaLabels, err := m.getPodSecurityLabels(ctx, opts.SubscriptionNamespace)
	if err != nil {
		return fmt.Errorf("failed to read PSA labels from namespace %s: %w", opts.SubscriptionNamespace, err)
	}

	// Create install namespace if it doesn't exist
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   opts.InstallNamespace,
			Labels: psaLabels,
		},
	}
	if err := m.Client.Create(ctx, ns); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("failed to create namespace %s: %w", opts.InstallNamespace, err)
		}
		// Namespace already exists — ensure PSA labels are applied
		if len(psaLabels) > 0 {
			if err := m.applyPodSecurityLabels(ctx, opts.InstallNamespace, psaLabels); err != nil {
				return fmt.Errorf("failed to apply PSA labels to namespace %s: %w", opts.InstallNamespace, err)
			}
		}
	}

	// Create ServiceAccount if it doesn't exist
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.ServiceAccountName(),
			Namespace: opts.InstallNamespace,
		},
	}
	if err := m.Client.Create(ctx, sa); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("failed to create ServiceAccount %s/%s: %w", opts.InstallNamespace, opts.ServiceAccountName(), err)
		}
	}

	// Create or update ClusterRoleBinding to cluster-admin.
	// We use server-side apply to ensure the binding always has the correct subject,
	// even if it already exists from a previous migration attempt with different settings.
	crbName := fmt.Sprintf("%s-cluster-admin", opts.ServiceAccountName())
	crb := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: crbName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      opts.ServiceAccountName(),
				Namespace: opts.InstallNamespace,
			},
		},
	}
	crbData, err := json.Marshal(crb)
	if err != nil {
		return fmt.Errorf("failed to marshal ClusterRoleBinding: %w", err)
	}
	crbObj := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: crbName}}
	if err := m.Client.Patch(ctx, crbObj, client.RawPatch(types.ApplyPatchType, crbData), client.ForceOwnership, client.FieldOwner(fieldManager)); err != nil {
		return fmt.Errorf("failed to apply ClusterRoleBinding %s: %w", crbName, err)
	}

	return nil
}

const podSecurityLabelPrefix = "pod-security.kubernetes.io/"

// getPodSecurityLabels reads Pod Security Admission labels from a namespace.
func (m *Migrator) getPodSecurityLabels(ctx context.Context, namespace string) (map[string]string, error) {
	var ns corev1.Namespace
	if err := m.Client.Get(ctx, types.NamespacedName{Name: namespace}, &ns); err != nil {
		return nil, err
	}

	psaLabels := make(map[string]string)
	for k, v := range ns.Labels {
		if strings.HasPrefix(k, podSecurityLabelPrefix) {
			psaLabels[k] = v
		}
	}
	return psaLabels, nil
}

// applyPodSecurityLabels merges PSA labels onto an existing namespace without removing other labels.
func (m *Migrator) applyPodSecurityLabels(ctx context.Context, namespace string, psaLabels map[string]string) error {
	var ns corev1.Namespace
	if err := m.Client.Get(ctx, types.NamespacedName{Name: namespace}, &ns); err != nil {
		return err
	}

	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}

	changed := false
	for k, v := range psaLabels {
		if ns.Labels[k] != v {
			ns.Labels[k] = v
			changed = true
		}
	}

	if changed {
		return m.Client.Update(ctx, &ns)
	}
	return nil
}

// RecoverFromBackup restores the Subscription from backup after a failed preparation or collection.
// This implements the RFC Recovery Before ClusterExtension Creation procedure.
func (m *Migrator) RecoverFromBackup(ctx context.Context, opts Options, backup *Backup) error {
	if backup == nil {
		return fmt.Errorf("no backup available for recovery")
	}

	// Scrub server-set fields from the Subscription backup
	sub := backup.Subscription.DeepCopy()
	sub.ResourceVersion = ""
	sub.UID = ""
	sub.Generation = 0
	sub.CreationTimestamp = metav1.Time{}
	sub.Status = operatorsv1alpha1.SubscriptionStatus{}

	// Update startingCSV to installedCSV if it was set
	if sub.Spec.StartingCSV != "" {
		sub.Spec.StartingCSV = backup.Subscription.Status.InstalledCSV
	}

	if err := m.Client.Create(ctx, sub); err != nil {
		return fmt.Errorf("failed to re-create Subscription: %w", err)
	}

	// Wait for Subscription to stabilize
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		var restored operatorsv1alpha1.Subscription
		if err := m.Client.Get(ctx, types.NamespacedName{
			Name:      opts.SubscriptionName,
			Namespace: opts.SubscriptionNamespace,
		}, &restored); err != nil {
			m.progress("Waiting for Subscription to appear...")
			return false, err
		}
		if restored.Status.State == operatorsv1alpha1.SubscriptionStateAtLatest ||
			restored.Status.State == operatorsv1alpha1.SubscriptionStateUpgradePending {
			return true, nil
		}
		m.progress(fmt.Sprintf("Subscription state: %s (waiting for AtLatestKnown)", restored.Status.State))
		return false, nil
	})
}

// RecoverBeforeCE implements recovery when CER creation fails (RFC Recovery Before ClusterExtension Creation).
// 1. Delete the CER with orphan cascading
// 2. Re-create the Subscription from backup
// 3. Wait for stabilization
func (m *Migrator) RecoverBeforeCE(ctx context.Context, opts Options, backup *Backup) error {
	// Delete the failed CER with orphan cascading
	cerName := fmt.Sprintf("%s-1", opts.ClusterExtensionName)
	cer := &ocv1.ClusterExtensionRevision{}
	cer.Name = cerName
	if err := m.Client.Delete(ctx, cer, client.PropagationPolicy(metav1.DeletePropagationOrphan)); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete CER during recovery: %w", err)
		}
	}

	return m.RecoverFromBackup(ctx, opts, backup)
}

// CreateClusterExtensionRevision builds and creates a CER from the collected resources.
func (m *Migrator) CreateClusterExtensionRevision(ctx context.Context, opts Options, info *MigrationInfo) error {
	cerName := fmt.Sprintf("%s-1", opts.ClusterExtensionName)

	// Build object list for phase sorting
	cerObjects := make([]ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration, 0, len(info.CollectedObjects))
	for _, obj := range info.CollectedObjects {
		stripped := stripResource(obj)
		cerObjects = append(cerObjects, *ocv1ac.ClusterExtensionRevisionObject().
			WithObject(stripped).
			WithCollisionProtection(ocv1.CollisionProtectionNone))
	}

	// Sort objects into phases
	phases := applier.PhaseSort(cerObjects)

	// Build the CER
	cerSpec := ocv1ac.ClusterExtensionRevisionSpec().
		WithRevision(1).
		WithCollisionProtection(ocv1.CollisionProtectionNone).
		WithLifecycleState(ocv1.ClusterExtensionRevisionLifecycleStateActive).
		WithPhases(phases...)

	// Labels: only OwnerKindKey and OwnerNameKey — this is how the CE controller
	// discovers CERs belonging to a ClusterExtension (via label selector).
	// All other metadata goes into annotations, matching buildClusterExtensionRevision
	// in the boxcutter applier.
	cerAnnotations := map[string]string{
		labels.ServiceAccountNameKey:      opts.ServiceAccountName(),
		labels.ServiceAccountNamespaceKey: opts.InstallNamespace,
		labels.PackageNameKey:             info.PackageName,
		labels.BundleNameKey:              info.BundleName,
		labels.BundleVersionKey:           info.Version,
	}
	if info.BundleImage != "" {
		cerAnnotations[labels.BundleReferenceKey] = info.BundleImage
	}

	cer := ocv1ac.ClusterExtensionRevision(cerName).
		WithSpec(cerSpec).
		WithLabels(map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: opts.ClusterExtensionName,
		}).
		WithAnnotations(cerAnnotations)

	// Apply via server-side apply
	cerObj := &ocv1.ClusterExtensionRevision{}
	cerObj.Name = cerName

	cerData, err := json.Marshal(cer)
	if err != nil {
		return fmt.Errorf("failed to marshal CER: %w", err)
	}

	if err := m.Client.Patch(ctx, cerObj, client.RawPatch(types.ApplyPatchType, cerData), client.ForceOwnership, client.FieldOwner(fieldManager)); err != nil {
		return fmt.Errorf("failed to apply ClusterExtensionRevision: %w", err)
	}

	return m.WaitForRevisionAvailable(ctx, cerName)
}

// WaitForRevisionAvailable waits for the CER to reach Available=True.
func (m *Migrator) WaitForRevisionAvailable(ctx context.Context, cerName string) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		var cer ocv1.ClusterExtensionRevision
		if err := m.Client.Get(ctx, types.NamespacedName{Name: cerName}, &cer); err != nil {
			m.progress(fmt.Sprintf("Waiting for CER %s (not found yet)", cerName))
			return false, err
		}

		available := ""
		progressing := ""
		for _, c := range cer.Status.Conditions {
			switch c.Type {
			case ocv1.ClusterExtensionRevisionTypeAvailable:
				if c.Status == metav1.ConditionTrue {
					return true, nil
				}
				if c.Reason == ocv1.ClusterExtensionRevisionReasonBlocked {
					return false, fmt.Errorf("ClusterExtensionRevision %s is blocked: %s", cerName, c.Message)
				}
				available = fmt.Sprintf("%s (%s)", c.Status, c.Reason)
			case ocv1.ClusterExtensionRevisionTypeProgressing:
				progressing = fmt.Sprintf("%s (%s)", c.Status, c.Reason)
			}
		}

		if available != "" || progressing != "" {
			m.progress(fmt.Sprintf("CER %s — Available: %s, Progressing: %s", cerName, available, progressing))
		} else {
			m.progress(fmt.Sprintf("Waiting for CER %s to be reconciled...", cerName))
		}
		return false, nil
	})
}

// CreateClusterExtension creates a CE that adopts the CER (RFC Step 7).
func (m *Migrator) CreateClusterExtension(ctx context.Context, opts Options, info *MigrationInfo) error {
	ce := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: opts.ClusterExtensionName,
		},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: opts.InstallNamespace,
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: opts.ServiceAccountName(),
			},
			Source: ocv1.SourceConfig{
				SourceType: ocv1.SourceTypeCatalog,
				Catalog: &ocv1.CatalogFilter{
					PackageName: info.PackageName,
				},
			},
		},
	}

	// Version pinning strategy:
	// - Manual approval (UpgradePending): pin to exact installed version — the user
	//   was controlling upgrades in OLMv0, so preserve that behavior.
	// - Automatic approval (AtLatestKnown): don't pin — allow OLMv1 to auto-upgrade
	//   within the channel, matching OLMv0's automatic upgrade behavior.
	if info.ManualApproval {
		ce.Spec.Source.Catalog.Version = info.Version
	}

	// RFC Step 7: set channel from Subscription (if set)
	if info.Channel != "" {
		ce.Spec.Source.Catalog.Channels = []string{info.Channel}
	}

	// RFC Step 7: set catalog selector to pin to the resolved ClusterCatalog
	if info.ResolvedCatalogName != "" {
		ce.Spec.Source.Catalog.Selector = &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"olm.operatorframework.io/metadata.name": info.ResolvedCatalogName,
			},
		}
	}

	if err := m.Client.Create(ctx, ce); err != nil {
		return fmt.Errorf("failed to create ClusterExtension: %w", err)
	}

	return m.WaitForClusterExtensionAvailable(ctx, opts.ClusterExtensionName)
}

// WaitForClusterExtensionAvailable waits for the CE to reach Installed=True.
func (m *Migrator) WaitForClusterExtensionAvailable(ctx context.Context, ceName string) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		var ce ocv1.ClusterExtension
		if err := m.Client.Get(ctx, types.NamespacedName{Name: ceName}, &ce); err != nil {
			m.progress(fmt.Sprintf("Waiting for CE %s (not found yet)", ceName))
			return false, err
		}

		installed := ""
		progressing := ""
		for _, c := range ce.Status.Conditions {
			switch c.Type {
			case "Installed":
				if c.Status == metav1.ConditionTrue {
					return true, nil
				}
				installed = fmt.Sprintf("%s (%s)", c.Status, c.Reason)
			case "Progressing":
				progressing = fmt.Sprintf("%s (%s)", c.Status, c.Reason)
			}
		}

		if installed != "" || progressing != "" {
			m.progress(fmt.Sprintf("CE %s — Installed: %s, Progressing: %s", ceName, installed, progressing))
		} else {
			m.progress(fmt.Sprintf("Waiting for CE %s to be reconciled...", ceName))
		}
		return false, nil
	})
}

// CleanupAction describes a single cleanup operation and its result.
type CleanupAction struct {
	Description string
	Succeeded   bool
	Skipped     bool
	Error       error
}

// CleanupResult holds the results of all cleanup operations.
type CleanupResult struct {
	Actions []CleanupAction
}

// CleanupOLMv0Resources removes remaining OLMv0 resources after migration (RFC Step 8).
func (m *Migrator) CleanupOLMv0Resources(ctx context.Context, opts Options, packageName, csvName string) *CleanupResult {
	result := &CleanupResult{}

	// 1. Delete the Operator CR
	operatorName := fmt.Sprintf("%s.%s", packageName, opts.SubscriptionNamespace)
	err := m.deleteOperatorCR(ctx, packageName, opts.SubscriptionNamespace)
	result.Actions = append(result.Actions, CleanupAction{
		Description: fmt.Sprintf("Delete Operator CR %s", operatorName),
		Succeeded:   err == nil,
		Error:       err,
	})

	// 2. Delete the OperatorCondition
	err = m.deleteOperatorCondition(ctx, csvName, opts.SubscriptionNamespace)
	result.Actions = append(result.Actions, CleanupAction{
		Description: fmt.Sprintf("Delete OperatorCondition %s/%s", opts.SubscriptionNamespace, csvName),
		Succeeded:   err == nil,
		Error:       err,
	})

	// 3. Delete copied CSVs
	copiedCount, err := m.deleteCopiedCSVs(ctx, csvName)
	if copiedCount > 0 {
		result.Actions = append(result.Actions, CleanupAction{
			Description: fmt.Sprintf("Delete %d copied CSV(s)", copiedCount),
			Succeeded:   err == nil,
			Error:       err,
		})
	} else {
		result.Actions = append(result.Actions, CleanupAction{
			Description: "Delete copied CSVs",
			Skipped:     true,
		})
	}

	// 4. OperatorGroup cleanup
	ogActions := m.cleanupOperatorGroup(ctx, opts)
	result.Actions = append(result.Actions, ogActions...)

	return result
}

func (m *Migrator) deleteCopiedCSVs(ctx context.Context, csvName string) (int, error) {
	var csvList operatorsv1alpha1.ClusterServiceVersionList
	if err := m.Client.List(ctx, &csvList,
		client.MatchingLabels{
			"olm.managed":    "true",
			"olm.copiedFrom": csvName,
		},
	); err != nil {
		return 0, err
	}

	deleted := 0
	for i := range csvList.Items {
		if err := m.Client.Delete(ctx, &csvList.Items[i], client.PropagationPolicy(metav1.DeletePropagationOrphan)); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return deleted, err
			}
		}
		deleted++
	}
	return deleted, nil
}

func (m *Migrator) deleteOperatorCR(ctx context.Context, packageName, namespace string) error {
	operatorName := fmt.Sprintf("%s.%s", packageName, namespace)
	op := &operatorsv1.Operator{}
	op.Name = operatorName
	if err := m.Client.Delete(ctx, op); err != nil {
		return client.IgnoreNotFound(err)
	}
	return nil
}

func (m *Migrator) deleteOperatorCondition(ctx context.Context, csvName, namespace string) error {
	oc := &operatorsv1.OperatorCondition{}
	oc.Name = csvName
	oc.Namespace = namespace
	if err := m.Client.Delete(ctx, oc); err != nil {
		return client.IgnoreNotFound(err)
	}
	return nil
}

// cleanupOperatorGroup deletes the OperatorGroup if no other Subscriptions remain in the namespace.
// Per RFC Step 8.4, also handles aggregation ClusterRoles.
func (m *Migrator) cleanupOperatorGroup(ctx context.Context, opts Options) []CleanupAction {
	var actions []CleanupAction

	// Check if other Subscriptions remain
	var subList operatorsv1alpha1.SubscriptionList
	if err := m.Client.List(ctx, &subList, client.InNamespace(opts.SubscriptionNamespace)); err != nil {
		actions = append(actions, CleanupAction{
			Description: "Check remaining Subscriptions",
			Error:       err,
		})
		return actions
	}

	if len(subList.Items) > 0 {
		actions = append(actions, CleanupAction{
			Description: fmt.Sprintf("Delete OperatorGroup (skipped: %d Subscription(s) remain)", len(subList.Items)),
			Skipped:     true,
		})
		return actions
	}

	// No other Subscriptions — find and handle the OperatorGroup
	var ogList operatorsv1.OperatorGroupList
	if err := m.Client.List(ctx, &ogList, client.InNamespace(opts.SubscriptionNamespace)); err != nil {
		actions = append(actions, CleanupAction{
			Description: "List OperatorGroups",
			Error:       err,
		})
		return actions
	}

	for i := range ogList.Items {
		og := &ogList.Items[i]

		// Strip olm.owner and olm.managed labels from aggregation ClusterRoles
		stripped := m.stripOGAggregationClusterRoles(ctx, og.Name)
		for _, name := range stripped {
			actions = append(actions, CleanupAction{
				Description: fmt.Sprintf("Strip OLM labels from aggregation ClusterRole %s", name),
				Succeeded:   true,
			})
		}

		// Delete the OperatorGroup
		err := m.Client.Delete(ctx, og)
		if err != nil && client.IgnoreNotFound(err) != nil {
			actions = append(actions, CleanupAction{
				Description: fmt.Sprintf("Delete OperatorGroup %s/%s", og.Namespace, og.Name),
				Error:       err,
			})
		} else {
			actions = append(actions, CleanupAction{
				Description: fmt.Sprintf("Delete OperatorGroup %s/%s", og.Namespace, og.Name),
				Succeeded:   true,
			})
		}
	}

	return actions
}

// stripOGAggregationClusterRoles strips olm.owner and olm.managed labels from
// OperatorGroup aggregation ClusterRoles (olm.og.<name>.<view|admin|edit>-<hash>).
// Returns the names of ClusterRoles that were updated.
func (m *Migrator) stripOGAggregationClusterRoles(ctx context.Context, ogName string) []string {
	prefix := fmt.Sprintf("olm.og.%s.", ogName)

	var crList unstructured.UnstructuredList
	crList.SetAPIVersion("rbac.authorization.k8s.io/v1")
	crList.SetKind("ClusterRoleList")

	if err := m.Client.List(ctx, &crList); err != nil {
		return nil
	}

	var stripped []string
	for _, cr := range crList.Items {
		if !strings.HasPrefix(cr.GetName(), prefix) {
			continue
		}

		lbls := cr.GetLabels()
		if lbls == nil {
			continue
		}

		changed := false
		for _, key := range []string{"olm.owner", "olm.owner.namespace", "olm.owner.kind", "olm.managed"} {
			if _, ok := lbls[key]; ok {
				delete(lbls, key)
				changed = true
			}
		}

		if changed {
			cr.SetLabels(lbls)
			if err := m.Client.Update(ctx, &cr); err == nil {
				stripped = append(stripped, cr.GetName())
			}
		}
	}
	return stripped
}

// FindCRDClusterRoles returns CRD-owned ClusterRoles that are not managed by OLMv1
// but should be retained to avoid breaking existing RBAC.
func (m *Migrator) FindCRDClusterRoles(ctx context.Context, csvName string) []string {
	var crList unstructured.UnstructuredList
	crList.SetAPIVersion("rbac.authorization.k8s.io/v1")
	crList.SetKind("ClusterRoleList")

	if err := m.Client.List(ctx, &crList); err != nil {
		return nil
	}

	var crdRoles []string
	for _, cr := range crList.Items {
		name := cr.GetName()
		lbls := cr.GetLabels()
		if lbls != nil && lbls["olm.owner"] == csvName {
			for _, suffix := range []string{"-admin", "-edit", "-view", "-crd"} {
				if strings.HasSuffix(name, suffix) {
					crdRoles = append(crdRoles, name)
					break
				}
			}
		}
	}
	return crdRoles
}

// stripResource removes server-side fields from a resource for inclusion in a CER.
func stripResource(obj unstructured.Unstructured) unstructured.Unstructured {
	stripped := unstructured.Unstructured{Object: make(map[string]interface{})}

	// Keep only essential top-level fields
	stripped.SetAPIVersion(obj.GetAPIVersion())
	stripped.SetKind(obj.GetKind())
	stripped.SetName(obj.GetName())
	if obj.GetNamespace() != "" {
		stripped.SetNamespace(obj.GetNamespace())
	}

	// Keep labels
	if labels := obj.GetLabels(); len(labels) > 0 {
		stripped.SetLabels(labels)
	}

	// Keep annotations, filtered
	if annotations := obj.GetAnnotations(); len(annotations) > 0 {
		filtered := filterAnnotations(annotations)
		if len(filtered) > 0 {
			stripped.SetAnnotations(filtered)
		}
	}

	// Keep spec
	if spec, ok := obj.Object["spec"]; ok {
		stripped.Object["spec"] = spec
		// Strip nested annotations (e.g., in Deployment pod template)
		stripNestedAnnotations(&stripped)
	}

	// Keep data/stringData for ConfigMaps/Secrets
	if data, ok := obj.Object["data"]; ok {
		stripped.Object["data"] = data
	}
	if stringData, ok := obj.Object["stringData"]; ok {
		stripped.Object["stringData"] = stringData
	}

	// Keep rules for ClusterRole/Role
	if rules, ok := obj.Object["rules"]; ok {
		stripped.Object["rules"] = rules
	}

	// Keep roleRef and subjects for bindings
	if roleRef, ok := obj.Object["roleRef"]; ok {
		stripped.Object["roleRef"] = roleRef
	}
	if subjects, ok := obj.Object["subjects"]; ok {
		stripped.Object["subjects"] = subjects
	}

	// Keep webhooks for webhook configurations
	if webhooks, ok := obj.Object["webhooks"]; ok {
		stripped.Object["webhooks"] = webhooks
	}

	return stripped
}

// filterAnnotations removes annotation prefixes that should not be migrated.
func filterAnnotations(annotations map[string]string) map[string]string {
	filtered := make(map[string]string)
	for k, v := range annotations {
		shouldStrip := false
		for _, prefix := range annotationPrefixesToStrip {
			if strings.HasPrefix(k, prefix) {
				shouldStrip = true
				break
			}
		}
		if !shouldStrip {
			filtered[k] = v
		}
	}
	return filtered
}

// stripNestedAnnotations removes deployment.kubernetes.io/ and kubectl.kubernetes.io/
// annotations from nested metadata (e.g., Deployment spec.template.metadata.annotations).
func stripNestedAnnotations(obj *unstructured.Unstructured) {
	templateAnnotations, found, _ := unstructured.NestedMap(obj.Object, "spec", "template", "metadata", "annotations")
	if found && templateAnnotations != nil {
		filtered := make(map[string]interface{})
		for k, v := range templateAnnotations {
			shouldStrip := false
			for _, prefix := range annotationPrefixesToStrip {
				if strings.HasPrefix(k, prefix) {
					shouldStrip = true
					break
				}
			}
			if !shouldStrip {
				filtered[k] = v
			}
		}
		if len(filtered) > 0 {
			_ = unstructured.SetNestedField(obj.Object, filtered, "spec", "template", "metadata", "annotations")
		} else {
			unstructured.RemoveNestedField(obj.Object, "spec", "template", "metadata", "annotations")
		}
	}
}
