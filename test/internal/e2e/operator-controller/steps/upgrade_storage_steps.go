package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	helmstorage "github.com/operator-framework/helm-operator-plugins/pkg/storage"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	deploymentutil "github.com/operator-framework/operator-controller/internal/operator-controller/clusterobjectdeployment"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

// RememberHelmExtensionIdentities reads live objects, since Helm's stored
// manifests do not contain the UIDs needed to detect deletion and recreation.
func RememberHelmExtensionIdentities(ctx context.Context) error {
	sc := shared.Scenario(ctx)
	if shared.FeatureGates[features.BoxcutterRuntime] {
		return fmt.Errorf("expected Helm-backed storage before OLM upgrade")
	}
	objects, err := listHelmReleaseResources(sc.ClusterExtensionName)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return fmt.Errorf("helm release has no managed resources")
	}
	for _, obj := range objects {
		live, err := shared.GetResource(ctx, obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), obj.GetNamespace())
		if err != nil {
			return err
		}
		if live.GetUID() == "" {
			return fmt.Errorf("resource %s has no live UID", client.ObjectKeyFromObject(live))
		}
		sc.UpgradeObjects = append(sc.UpgradeObjects, live)
	}
	// Pods are not bundle manifests. Preserve their identities through migration,
	// but allow a normal pod rollout when the bundle version changes afterward.
	return shared.WaitForUpgradeCheck(ctx, func(ctx context.Context) error {
		out, err := shared.K8sClient(ctx, "get", "pods", "-n", sc.Namespace, "-l", "app="+sc.ID, "-o", "json")
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
			if !shared.PodReady(&pod) {
				return fmt.Errorf("fixture pod %s is not ready before OLM upgrade", pod.Name)
			}
		}
		sc.UpgradePods = pods.Items
		return nil
	})
}

func ExtensionKeepsPreUpgradeIdentities(ctx context.Context) error {
	if err := checkExtensionIdentities(ctx, false); err != nil {
		return err
	}
	for _, before := range shared.Scenario(ctx).UpgradePods {
		out, err := shared.K8sClient(ctx, "get", "pod", before.Name, "-n", before.Namespace, "-o", "json")
		if err != nil {
			return err
		}
		var after corev1.Pod
		if err := json.Unmarshal([]byte(out), &after); err != nil {
			return err
		}
		if after.UID != before.UID || !shared.PodReady(&after) {
			return fmt.Errorf("workload pod %s was replaced or is no longer ready after OLM upgrade", before.Name)
		}
	}
	return nil
}

func RetainedExtensionKeepsPreUpgradeIdentities(ctx context.Context) error {
	return checkExtensionIdentities(ctx, true)
}

func checkExtensionIdentities(ctx context.Context, afterUpdate bool) error {
	sc := shared.Scenario(ctx)
	if len(sc.UpgradeObjects) == 0 {
		return fmt.Errorf("no pre-upgrade extension resource identities remembered")
	}
	retained := map[string]bool{}
	if afterUpdate {
		objects, err := listExtensionResources(ctx, sc.ClusterExtensionName)
		if err != nil {
			return err
		}
		for _, obj := range objects {
			retained[fmt.Sprintf("%s/%s", obj.GetObjectKind().GroupVersionKind().Kind, client.ObjectKeyFromObject(obj))] = true
		}
	}
	for _, before := range sc.UpgradeObjects {
		// Rendered RBAC names incorporate the CSV version. Those resources are
		// legitimately replaced by an extension update; all other fixture names
		// are stable and must retain their UIDs, including the Deployment and CRD.
		if afterUpdate && before.GroupVersionKind().Group == "rbac.authorization.k8s.io" &&
			!retained[fmt.Sprintf("%s/%s", before.GetKind(), client.ObjectKeyFromObject(before))] {
			continue
		}
		after, err := shared.GetResource(ctx, before.GetKind(), before.GetName(), before.GetNamespace())
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

// PreExistingExtensionStorageTransitioned checks the storage used after OLM upgrades.
func PreExistingExtensionStorageTransitioned(ctx context.Context) error {
	return checkUpgradedExtensionStorage(ctx, upgradeStorageExpectation{version: "1.0.0"})
}

func UpdatedExtensionStorageIsHealthy(ctx context.Context) error {
	return checkUpgradedExtensionStorage(ctx, upgradeStorageExpectation{version: "1.0.1", afterUpdate: true})
}

type upgradeStorageExpectation struct {
	version     string
	afterUpdate bool
}

func checkUpgradedExtensionStorage(ctx context.Context, want upgradeStorageExpectation) error {
	sc := shared.Scenario(ctx)
	if !shared.FeatureGates[features.BoxcutterRuntime] {
		secret, err := helmReleaseSecretForExtension(sc.ClusterExtensionName)
		if err != nil {
			return err
		}
		rel, err := helmReleaseFromSecret(secret)
		if err != nil {
			return err
		}
		if rel.Labels[labels.BundleVersionKey] != want.version {
			return fmt.Errorf("helm release bundle version = %q, want %q", rel.Labels[labels.BundleVersionKey], want.version)
		}
		return nil
	}

	const (
		adoptionRevision   int64 = 1
		protectionRevision int64 = 2
		migratedVersion          = "1.0.0"
	)
	return shared.WaitForUpgradeCheck(ctx, func(ctx context.Context) error {
		ext, err := shared.GetResource(ctx, "clusterextension", sc.ClusterExtensionName, "")
		if err != nil {
			return err
		}
		obj, err := shared.GetResource(ctx, "clusterobjectdeployment", sc.ClusterExtensionName, "")
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
		available := shared.CurrentCondition(cod.Generation, cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeAvailable)
		if !shared.ConditionMatches(available, metav1.ConditionTrue, nil, nil) || !deploymentutil.RolloutComplete(&cod) {
			return fmt.Errorf("COD %s has not completed migration/update: conditions=%+v", cod.Name, cod.Status.Conditions)
		}
		if cod.Spec.Template.Spec.CollisionProtection != ocv1.CollisionProtectionPrevent {
			return fmt.Errorf("COD template collision protection = %s, want Prevent", cod.Spec.Template.Spec.CollisionProtection)
		}

		revisions, err := shared.ListClusterObjectSetsInGroup(ctx, sc.ClusterExtensionName)
		if err != nil {
			return err
		}
		byNumber := make(map[int64]*ocv1.ClusterObjectSet, len(revisions))
		storageUIDs := map[string]types.UID{cod.Name: cod.UID}
		var active *ocv1.ClusterObjectSet
		for i := range revisions {
			rev := &revisions[i]
			number := rev.Spec.Revision
			if rev.Spec.Group != cod.Name || number < 1 || byNumber[number] != nil {
				return fmt.Errorf("unexpected group/revision for COS %s: %s/%d", rev.Name, rev.Spec.Group, number)
			}
			byNumber[number] = rev
			storageUIDs[rev.Name] = rev.UID
			if err := requireController(rev, &cod); err != nil {
				return err
			}
			protection := ocv1.CollisionProtectionPrevent
			if number == adoptionRevision {
				protection = ocv1.CollisionProtectionNone
			}
			if rev.Spec.CollisionProtection != protection || rev.Status.CompletedAt.IsZero() {
				return fmt.Errorf("COS %s: protection=%s completedAt=%s, want %s and completed rollout",
					rev.Name, rev.Spec.CollisionProtection, rev.Status.CompletedAt, protection)
			}
			ready := shared.CurrentCondition(rev.Generation, rev.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
			switch rev.Spec.LifecycleState {
			case ocv1.ClusterObjectSetLifecycleStateActive:
				if active != nil {
					return fmt.Errorf("multiple active revisions: %s and %s", active.Name, rev.Name)
				}
				if !shared.ConditionMatches(ready, metav1.ConditionTrue, nil, nil) || rev.Annotations[labels.BundleVersionKey] != want.version {
					return fmt.Errorf("COS %s is not active/ready at bundle version %s: conditions=%+v", rev.Name, want.version, rev.Status.Conditions)
				}
				active = rev
			case ocv1.ClusterObjectSetLifecycleStateArchived:
				if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != ocv1.ClusterObjectSetReasonArchived {
					return fmt.Errorf("COS %s is not archived: conditions=%+v", rev.Name, rev.Status.Conditions)
				}
			default:
				return fmt.Errorf("COS %s lifecycle state is %q", rev.Name, rev.Spec.LifecycleState)
			}
		}
		if active == nil {
			return fmt.Errorf("no active revision at bundle version %s", want.version)
		}
		adopting, protecting := byNumber[adoptionRevision], byNumber[protectionRevision]
		if adopting == nil || protecting == nil || adopting.Spec.LifecycleState != ocv1.ClusterObjectSetLifecycleStateArchived ||
			adopting.Annotations[labels.BundleVersionKey] != migratedVersion || protecting.Annotations[labels.BundleVersionKey] != migratedVersion {
			return fmt.Errorf("expected archived adoption and completed protection revisions for Helm bundle %s", migratedVersion)
		}
		if !reflect.DeepEqual(adopting.Spec.Phases, protecting.Spec.Phases) ||
			!reflect.DeepEqual(adopting.Spec.ProgressionProbes, protecting.Spec.ProgressionProbes) ||
			!reflect.DeepEqual(adopting.Annotations, protecting.Annotations) {
			return fmt.Errorf("migration revisions %s and %s have different content", adopting.Name, protecting.Name)
		}
		if err := checkHelmHistoryRemoved(ctx, ext); err != nil {
			return err
		}

		objects, err := listExtensionRevisionResources(ctx, sc.ClusterExtensionName)
		if err != nil {
			return err
		}
		if len(objects) == 0 {
			return fmt.Errorf("active revision %s contains no objects", active.Name)
		}
		for _, desired := range objects {
			live, err := shared.GetResource(ctx, desired.GetObjectKind().GroupVersionKind().Kind, desired.GetName(), desired.GetNamespace())
			if err != nil {
				return err
			}
			if err := requireController(live, active); err != nil {
				return err
			}
		}
		if want.afterUpdate {
			for name, uid := range sc.UpgradeStorageUIDs {
				if storageUIDs[name] != uid {
					return fmt.Errorf("storage object %s UID changed from %s to %s during extension update", name, uid, storageUIDs[name])
				}
			}
		} else {
			sc.UpgradeStorageUIDs = storageUIDs
		}
		return nil
	})
}

func checkHelmHistoryRemoved(ctx context.Context, ext client.Object) error {
	out, err := shared.K8sClient(ctx, "get", "secrets", "-n", shared.OLMNamespace,
		"-l", fmt.Sprintf("owner=operator-controller,type=index,name=%s", ext.GetName()),
		"--field-selector", "type="+string(helmstorage.SecretTypeChunkedIndex), "-o", "json")
	if err != nil {
		return err
	}
	var indexes corev1.SecretList
	if err := json.Unmarshal([]byte(out), &indexes); err != nil {
		return err
	}
	for i := range indexes.Items {
		if metav1.IsControlledBy(&indexes.Items[i], ext) {
			return fmt.Errorf("stored Helm release index Secret %s still exists", indexes.Items[i].Name)
		}
	}
	return nil
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
