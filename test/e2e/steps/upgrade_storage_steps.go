package steps

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

// RememberHelmExtensionIdentities reads live objects, since Helm's stored
// manifests do not contain the UIDs needed to detect deletion and recreation.
func RememberHelmExtensionIdentities(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	if featureGates[features.BoxcutterRuntime] {
		return fmt.Errorf("expected Helm-backed storage before OLM upgrade")
	}
	objects, err := listHelmReleaseResources(sc.clusterExtensionName)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return fmt.Errorf("helm release has no managed resources")
	}
	for _, obj := range objects {
		live, err := getResource(obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), obj.GetNamespace())
		if err != nil {
			return err
		}
		if live.GetUID() == "" {
			return fmt.Errorf("resource %s has no live UID", client.ObjectKeyFromObject(live))
		}
		sc.upgradeObjects = append(sc.upgradeObjects, live)
	}
	// Pods are not bundle manifests. Preserve their identities through migration,
	// but allow a normal pod rollout when the bundle version changes afterward.
	return waitForUpgradeCheck(ctx, func() error {
		out, err := k8sClient(ctx, "get", "pods", "-n", sc.namespace, "-l", "app="+sc.id, "-o", "json")
		if err != nil {
			return err
		}
		var pods corev1.PodList
		if err := json.Unmarshal([]byte(out), &pods); err != nil {
			return err
		}
		if len(pods.Items) == 0 {
			return fmt.Errorf("no fixture workload pods found before OLM upgrade")
		}
		for _, pod := range pods.Items {
			if !podReady(&pod) {
				return fmt.Errorf("fixture pod %s is not ready before OLM upgrade", pod.Name)
			}
		}
		sc.upgradePods = pods.Items
		return nil
	})
}

func podReady(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func ExtensionKeepsPreUpgradeIdentities(ctx context.Context) error {
	if err := checkExtensionIdentities(ctx, false); err != nil {
		return err
	}
	for _, before := range scenarioCtx(ctx).upgradePods {
		out, err := k8sClient(ctx, "get", "pod", before.Name, "-n", before.Namespace, "-o", "json")
		if err != nil {
			return err
		}
		var after corev1.Pod
		if err := json.Unmarshal([]byte(out), &after); err != nil {
			return err
		}
		if after.UID != before.UID || !podReady(&after) {
			return fmt.Errorf("workload pod %s was replaced or is no longer ready after OLM upgrade", before.Name)
		}
	}
	return nil
}

func RetainedExtensionKeepsPreUpgradeIdentities(ctx context.Context) error {
	return checkExtensionIdentities(ctx, true)
}

func checkExtensionIdentities(ctx context.Context, afterUpdate bool) error {
	sc := scenarioCtx(ctx)
	if len(sc.upgradeObjects) == 0 {
		return fmt.Errorf("no pre-upgrade extension resource identities remembered")
	}
	retained := map[string]bool{}
	if afterUpdate {
		objects, err := listExtensionResources(sc.clusterExtensionName)
		if err != nil {
			return err
		}
		for _, obj := range objects {
			retained[fmt.Sprintf("%s/%s", obj.GetObjectKind().GroupVersionKind().Kind, client.ObjectKeyFromObject(obj))] = true
		}
	}
	for _, before := range sc.upgradeObjects {
		// Rendered RBAC names incorporate the CSV version. Those resources are
		// legitimately replaced by an extension update; all other fixture names
		// are stable and must retain their UIDs, including the Deployment and CRD.
		if afterUpdate && before.GroupVersionKind().Group == "rbac.authorization.k8s.io" &&
			!retained[fmt.Sprintf("%s/%s", before.GetKind(), client.ObjectKeyFromObject(before))] {
			continue
		}
		after, err := getResource(before.GetKind(), before.GetName(), before.GetNamespace())
		if err != nil {
			return err
		}
		if after.GetUID() != before.GetUID() {
			return fmt.Errorf("%s %s was replaced: UID changed from %s to %s", before.GetKind(),
				client.ObjectKeyFromObject(before), before.GetUID(), after.GetUID())
		}
	}
	return nil
}

// PreExistingExtensionStorageTransitioned expects a complete CE -> COD -> COS
// handoff for experimental OLM. Standard upgrades must keep using Helm storage.
func PreExistingExtensionStorageTransitioned(ctx context.Context) error {
	return checkUpgradedExtensionStorage(ctx, 2, "1.0.0")
}

func UpdatedExtensionStorageIsHealthy(ctx context.Context) error {
	return checkUpgradedExtensionStorage(ctx, 3, "1.0.1")
}

func checkUpgradedExtensionStorage(ctx context.Context, revision int64, version string) error {
	sc := scenarioCtx(ctx)
	if !featureGates[features.BoxcutterRuntime] {
		// Do not request experimental APIs on standard installations. Reading
		// the live Helm release also verifies that updating the same CE still
		// writes Helm storage, rather than accepting only retained CE status.
		secret, err := helmReleaseSecretForExtension(sc.clusterExtensionName)
		if err != nil {
			return err
		}
		if secret == nil {
			return fmt.Errorf("no deployed Helm release for %s", sc.clusterExtensionName)
		}
		rel, err := helmReleaseFromSecret(secret)
		if err != nil {
			return err
		}
		if rel.Labels[labels.BundleVersionKey] != version {
			return fmt.Errorf("helm release bundle version = %q, want %q", rel.Labels[labels.BundleVersionKey], version)
		}
		return nil
	}

	return waitForUpgradeCheck(ctx, func() error {
		ext, err := getResource("clusterextension", sc.clusterExtensionName, "")
		if err != nil {
			return err
		}
		obj, err := getResource("clusterobjectdeployment", sc.clusterExtensionName, "")
		if err != nil {
			return err
		}
		var cod ocv1.ClusterObjectDeployment
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &cod); err != nil {
			return err
		}
		if err := requireController(&cod, ext); err != nil {
			return err
		}
		if revision == 3 && sc.upgradeStorageUIDs["cod"] != cod.UID {
			return fmt.Errorf("COD %s UID changed during extension update", cod.Name)
		}
		if !conditionMatches(currentCondition(cod.Generation, cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeAvailable), metav1.ConditionTrue, nil, nil) ||
			!conditionMatches(currentCondition(cod.Generation, cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeProgressing), metav1.ConditionFalse, ptr.To(ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed), nil) {
			return fmt.Errorf("COD %s has not completed migration/update: conditions=%+v", cod.Name, cod.Status.Conditions)
		}
		if cod.Spec.Template.Spec.CollisionProtection != ocv1.CollisionProtectionPrevent {
			return fmt.Errorf("COD template collision protection = %s, want Prevent", cod.Spec.Template.Spec.CollisionProtection)
		}
		revisions, err := listClusterObjectSetsInGroup(ctx, sc.clusterExtensionName)
		if err != nil {
			return err
		}
		if int64(len(revisions)) != revision {
			return fmt.Errorf("group %s has %d revisions, want %d", sc.clusterExtensionName, len(revisions), revision)
		}
		seen := map[int64]bool{}
		var active *ocv1.ClusterObjectSet
		for i := range revisions {
			rev := &revisions[i]
			if rev.Spec.Group != cod.Name || rev.Spec.Revision < 1 || rev.Spec.Revision > revision || seen[rev.Spec.Revision] {
				return fmt.Errorf("unexpected group/revision for COS %s: %s/%d", rev.Name, rev.Spec.Group, rev.Spec.Revision)
			}
			seen[rev.Spec.Revision] = true
			if err := requireController(rev, &cod); err != nil {
				return err
			}
			if revision == 3 && rev.Spec.Revision < revision && sc.upgradeStorageUIDs[rev.Name] != rev.UID {
				return fmt.Errorf("COS %s UID changed during extension update", rev.Name)
			}
			protection := ocv1.CollisionProtectionPrevent
			if rev.Spec.Revision == 1 {
				protection = ocv1.CollisionProtectionNone
				if rev.Labels[labels.MigratedFromHelmKey] != "true" || rev.Annotations[labels.BundleVersionKey] != "1.0.0" {
					return fmt.Errorf("COS %s is not the migrated 1.0.0 Helm release", rev.Name)
				}
			}
			if rev.Spec.CollisionProtection != protection || rev.Status.CompletedAt.IsZero() {
				return fmt.Errorf("COS %s: protection=%s completedAt=%s, want %s and completed rollout", rev.Name,
					rev.Spec.CollisionProtection, rev.Status.CompletedAt, protection)
			}
			if rev.Spec.Revision == revision {
				active = rev
				if rev.Spec.LifecycleState != ocv1.ClusterObjectSetLifecycleStateActive ||
					!conditionMatches(currentCondition(rev.Generation, rev.Status.Conditions, ocv1.ClusterObjectSetTypeReady), metav1.ConditionTrue, nil, nil) ||
					rev.Annotations[labels.BundleVersionKey] != version {
					return fmt.Errorf("COS %s is not active/ready at bundle version %s: conditions=%+v", rev.Name, version, rev.Status.Conditions)
				}
			} else if rev.Spec.LifecycleState != ocv1.ClusterObjectSetLifecycleStateArchived ||
				!conditionMatches(currentCondition(rev.Generation, rev.Status.Conditions, ocv1.ClusterObjectSetTypeReady), metav1.ConditionFalse, ptr.To(ocv1.ClusterObjectSetReasonArchived), nil) {
				return fmt.Errorf("COS %s is not archived: conditions=%+v", rev.Name, rev.Status.Conditions)
			}
		}
		if active == nil {
			return fmt.Errorf("no active revision %d", revision)
		}
		objects, err := listExtensionRevisionResources(sc.clusterExtensionName)
		if err != nil {
			return err
		}
		if len(objects) == 0 {
			return fmt.Errorf("active revision %s contains no objects", active.Name)
		}
		for _, desired := range objects {
			live, err := getResource(desired.GetObjectKind().GroupVersionKind().Kind, desired.GetName(), desired.GetNamespace())
			if err != nil {
				return err
			}
			if err := requireController(live, active); err != nil {
				return err
			}
		}
		if revision == 2 {
			sc.upgradeStorageUIDs = map[string]types.UID{"cod": cod.UID}
			for _, rev := range revisions {
				sc.upgradeStorageUIDs[rev.Name] = rev.UID
			}
		}
		return nil
	})
}

func requireController(object, controller client.Object) error {
	owner := metav1.GetControllerOf(object)
	gvk := controller.GetObjectKind().GroupVersionKind()
	// Typed list items may omit TypeMeta. The controller identity is still
	// unambiguous from the API type and its live name/UID.
	switch controller.(type) {
	case *ocv1.ClusterObjectDeployment:
		gvk = ocv1.GroupVersion.WithKind(ocv1.ClusterObjectDeploymentKind)
	case *ocv1.ClusterObjectSet:
		gvk = ocv1.GroupVersion.WithKind(ocv1.ClusterObjectSetKind)
	}
	if owner == nil || owner.APIVersion != gvk.GroupVersion().String() || owner.Kind != gvk.Kind ||
		owner.Name != controller.GetName() || owner.UID != controller.GetUID() {
		return fmt.Errorf("%s %s controller=%+v, want %s %s (UID %s)", object.GetObjectKind().GroupVersionKind().Kind,
			object.GetName(), owner, gvk.Kind, controller.GetName(), controller.GetUID())
	}
	return nil
}
