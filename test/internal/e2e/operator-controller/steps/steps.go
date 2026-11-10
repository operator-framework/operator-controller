package steps

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sresource "k8s.io/cli-runtime/pkg/resource"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

// RegisterSteps registers ClusterExtension lifecycle and integration assertions.
func RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^(?i)bundle "([^"]+)" is installed in version "([^"]+)"$`, BundleInstalled)
	sc.Step(`^(?i)ClusterExtension is applied(?:\s+.*)?$`, shared.ResourceIsApplied)
	sc.Step(`^(?i)ClusterExtension version is updated to "([^"]+)"$`, ClusterExtensionVersionUpdate)
	sc.Step(`^(?i)ClusterExtension is updated(?:\s+.*)?$`, shared.ResourceIsApplied)
	sc.Step(`^(?i)ClusterExtension is available$`, ClusterExtensionIsAvailable)
	sc.Step(`^(?i)ClusterExtension is rolled out$`, ClusterExtensionIsRolledOut)
	sc.Step(`^(?i)ClusterExtension resources are created and labeled$`, ClusterExtensionResourcesCreatedAndAreLabeled)
	sc.Step(`^(?i)ClusterExtension is removed$`, ClusterExtensionIsRemoved)
	sc.Step(`^(?i)ClusterExtension (?:latest generation )?has (?:been )?reconciled(?: the latest generation)?$`, ClusterExtensionReconciledLatestGeneration)
	sc.Step(`^(?i)the ClusterExtension's constituent resources are removed$`, ClusterExtensionResourcesRemoved)
	sc.Step(`^(?i)ClusterExtension reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+) and Message:$`, ClusterExtensionReportsCondition)
	sc.Step(`^(?i)ClusterExtension reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+) and Message includes:$`, ClusterExtensionReportsConditionWithMessageFragment)
	sc.Step(`^(?i)ClusterExtension reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+)$`, ClusterExtensionReportsConditionWithoutMsg)
	sc.Step(`^(?i)ClusterExtension reports ([[:alnum:]]+) as ([[:alnum:]]+)$`, ClusterExtensionReportsConditionWithoutReason)
	sc.Step(`^(?i)ClusterExtension reports ([[:alnum:]]+) transition between (\d+) and (\d+) minutes since ClusterObjectSet "([^"]+)" was created$`, ClusterExtensionReportsConditionTransitionTime)
	sc.Step(`^(?i)ClusterExtension reconciliation is triggered$`, TriggerClusterExtensionReconciliation)
	sc.Step(`^(?i)namespace "([^"]+)" is managed by OLM$`, NamespaceIsManagedByOLM)
	sc.Step(`^(?i)operator "([^"]+)" target namespace is "([^"]+)"$`, OperatorTargetNamespace)
	sc.Step(`^(?i)ClusterExtension "([^"]+)" owns (\d+) ClusterObjectSets?$`, ClusterExtensionOwnsClusterObjectSets)
	sc.Step(`^(?i)ClusterExtension "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+)$`, NamedClusterExtensionReportsCondition)
	sc.Step(`^(?i)ClusterExtension "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+) and Message includes:$`, NamedClusterExtensionReportsConditionWithMessageFragment)
	sc.Step(`^(?i)ClusterExtension is reconciled$`, shared.ClusterExtensionIsReconciled)
}

// BundleInstalled waits for the ClusterExtension to report the specified bundle name and version as installed. Polls with timeout.
func BundleInstalled(ctx context.Context, name, version string) error {
	sc := shared.Scenario(ctx)
	name = shared.SubstituteScenarioVars(name, sc)
	shared.WaitFor(ctx, func() bool {
		v, err := shared.K8sClient(ctx, "get", "clusterextension", sc.ClusterExtensionName, "-o", "json")
		if err != nil {
			return false
		}
		var ext ocv1.ClusterExtension
		if err := json.Unmarshal([]byte(v), &ext); err != nil {
			return false
		}
		installed := shared.CurrentCondition(ext.Generation, ext.Status.Conditions, ocv1.TypeInstalled)
		return shared.ConditionMatches(installed, metav1.ConditionTrue, nil, nil) &&
			ext.Status.Install.Bundle.Name == name && ext.Status.Install.Bundle.Version == version
	})
	return nil
}

// ClusterExtensionIsAvailable waits for the ClusterExtension's Installed condition to be True. Polls with timeout.
func ClusterExtensionIsAvailable(ctx context.Context) error {
	sc := shared.Scenario(ctx)
	if err := waitForExtensionCondition(ctx, ocv1.TypeInstalled, metav1.ConditionTrue, nil, nil); err != nil {
		return err
	}

	if rec := shared.CommandRecorderFromContext(ctx); rec != nil {
		out, _ := shared.K8sClient(ctx, "get", "clusterextension", sc.ClusterExtensionName, "-o", "jsonpath={.status}")
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, []byte(out), "", "  "); err == nil {
			out = pretty.String()
		}
		rec.RecordCustom(
			fmt.Sprintf("kubectl get clusterextension %s -o jsonpath='{.status}' | jq .", sc.ClusterExtensionName),
			out+"\n", "",
		)
	}
	return nil
}

// ClusterExtensionIsRemoved deletes the current ClusterExtension, saving its state for potential restore checks.
func ClusterExtensionIsRemoved(ctx context.Context) error {
	sc := shared.Scenario(ctx)
	return shared.ResourceRemoved(ctx, fmt.Sprintf("clusterextension/%s", sc.ClusterExtensionName))
}

// ClusterExtensionIsRolledOut waits for the ClusterExtension's Progressing condition to be True with reason Succeeded,
// for the current generation, then gathers its constituent resources into the scenario context. Polls with timeout.
func ClusterExtensionIsRolledOut(ctx context.Context) error {
	sc := shared.Scenario(ctx)
	if err := waitForExtensionCondition(ctx, ocv1.TypeProgressing, metav1.ConditionTrue, ptr.To(ocv1.ReasonSucceeded), nil); err != nil {
		return err
	}

	if rec := shared.CommandRecorderFromContext(ctx); rec != nil {
		out, _ := shared.K8sClient(ctx, "get", "clusterextension", sc.ClusterExtensionName)
		rec.RecordCustom(fmt.Sprintf("kubectl get clusterextension %s", sc.ClusterExtensionName), out, "")
	}

	// Save ClusterExtension resources to test context for posterior checks
	if err := gatherClusterExtensionObjects(sc); err != nil {
		return err
	}
	return nil
}

// ClusterExtensionOwnsClusterObjectSets waits for the named ClusterExtension to own exactly the
// expected number of ClusterObjectSets. Polls with timeout.
func ClusterExtensionOwnsClusterObjectSets(ctx context.Context, extName string, expectedCount int) error {
	sc := shared.Scenario(ctx)
	extName = shared.SubstituteScenarioVars(extName, sc)
	shared.WaitFor(ctx, func() bool {
		revisions, err := shared.ListClusterObjectSetsInGroup(ctx, extName)
		if err != nil {
			return false
		}
		if len(revisions) != expectedCount {
			return false
		}
		ext, err := shared.GetResource("clusterextension", extName, "")
		if err != nil {
			return false
		}
		for _, rev := range revisions {
			owner := metav1.GetControllerOf(&rev)
			if owner == nil || owner.Kind != ocv1.ClusterExtensionKind || owner.Name != extName || owner.UID != ext.GetUID() {
				return false
			}
		}
		return true
	})
	return nil
}

// ClusterExtensionReconciledLatestGeneration waits for the Progressing condition
// to observe the current generation, regardless of its status or reason. This
// confirms the spec was processed, not that the rollout succeeded.
func ClusterExtensionReconciledLatestGeneration(ctx context.Context) error {
	sc := shared.Scenario(ctx)
	shared.WaitFor(ctx, func() bool {
		resource, err := shared.ReadResourceConditions(ctx, "clusterextension", sc.ClusterExtensionName)
		if err != nil {
			return false
		}
		return shared.CurrentCondition(resource.Metadata.Generation, resource.Status.Conditions, ocv1.TypeProgressing) != nil
	})
	return nil
}

// ClusterExtensionReportsCondition waits for the ClusterExtension to have a condition matching the specified type,
// status, reason, and exact message. Polls with timeout.
func ClusterExtensionReportsCondition(ctx context.Context, conditionType, conditionStatus, conditionReason string, msg *godog.DocString) error {
	return waitForExtensionCondition(ctx, conditionType, metav1.ConditionStatus(conditionStatus), &conditionReason, shared.MessageComparison(ctx, msg))
}

// ClusterExtensionReportsConditionTransitionTime asserts that a condition's lastTransitionTime falls within
// the specified minute range since the ClusterObjectSet's creation, when its progress deadline starts.
func ClusterExtensionReportsConditionTransitionTime(ctx context.Context, conditionType string, minMinutes, maxMinutes int, revisionName string) error {
	sc := shared.Scenario(ctx)
	t := godog.T(ctx)

	revisionName = shared.SubstituteScenarioVars(revisionName, sc)
	creationTimestampValue, err := shared.K8sClient(ctx, "get", "clusterobjectset", revisionName, "-o", "jsonpath={.metadata.creationTimestamp}")
	require.NoError(t, err)
	creationTimestamp, err := time.Parse(time.RFC3339, creationTimestampValue)
	require.NoError(t, err, "failed to parse ClusterObjectSet creationTimestamp")

	var condition *metav1.Condition
	shared.WaitFor(ctx, func() bool {
		resource, err := shared.ReadResourceConditions(ctx, "clusterextension", sc.ClusterExtensionName)
		if err != nil {
			return false
		}
		condition = shared.CurrentCondition(resource.Metadata.Generation, resource.Status.Conditions, conditionType)
		return condition != nil && !condition.LastTransitionTime.IsZero()
	})
	lastTransitionTime := condition.LastTransitionTime.Time

	transitionDuration := lastTransitionTime.Sub(creationTimestamp)
	minDuration := time.Duration(minMinutes) * time.Minute
	maxDuration := time.Duration(maxMinutes) * time.Minute

	require.GreaterOrEqual(t, transitionDuration, minDuration,
		"condition %s transitioned too early: %v since ClusterObjectSet %s creation (expected >= %v)", conditionType, transitionDuration, revisionName, minDuration)
	require.LessOrEqual(t, transitionDuration, maxDuration,
		"condition %s transitioned too late: %v since ClusterObjectSet %s creation (expected <= %v)", conditionType, transitionDuration, revisionName, maxDuration)

	return nil
}

// ClusterExtensionReportsConditionWithMessageFragment waits for the ClusterExtension to have a condition matching
// type, status, and reason, with a message containing the specified fragment. Polls with timeout.
func ClusterExtensionReportsConditionWithMessageFragment(ctx context.Context, conditionType, conditionStatus, conditionReason string, msgFragment *godog.DocString) error {
	return waitForExtensionCondition(ctx, conditionType, metav1.ConditionStatus(conditionStatus), &conditionReason, shared.MessageFragmentComparison(ctx, msgFragment))
}

// ClusterExtensionReportsConditionWithoutMsg waits for the ClusterExtension to have a condition matching type,
// status, and reason, without checking the message. Polls with timeout.
func ClusterExtensionReportsConditionWithoutMsg(ctx context.Context, conditionType, conditionStatus, conditionReason string) error {
	return ClusterExtensionReportsCondition(ctx, conditionType, conditionStatus, conditionReason, nil)
}

// ClusterExtensionReportsConditionWithoutReason waits for the ClusterExtension to have a condition matching type
// and status, without checking reason or message. Polls with timeout.
func ClusterExtensionReportsConditionWithoutReason(ctx context.Context, conditionType, conditionStatus string) error {
	return waitForExtensionCondition(ctx, conditionType, metav1.ConditionStatus(conditionStatus), nil, nil)
}

// ClusterExtensionResourcesCreatedAndAreLabeled verifies each constituent resource has the expected OLM owner-kind
// and owner-name labels. Polls with timeout per resource.
func ClusterExtensionResourcesCreatedAndAreLabeled(ctx context.Context) error {
	sc := shared.Scenario(ctx)
	if len(sc.ExtensionObjects) == 0 {
		return fmt.Errorf("extension objects not found in context")
	}

	for _, obj := range sc.ExtensionObjects {
		shared.WaitFor(ctx, func() bool {
			kind := obj.GetObjectKind().GroupVersionKind().Kind
			clusterObj, err := shared.GetResource(kind, obj.GetName(), obj.GetNamespace())
			if err != nil {
				shared.Log().V(1).Error(err, "error getting resource", "name", obj.GetName(), "namespace", obj.GetNamespace(), "kind", kind)
				return false
			}

			labels := clusterObj.GetLabels()
			if labels == nil {
				shared.Log().V(1).Info("no labels found for resource", "name", obj.GetName(), "namespace", obj.GetNamespace(), "kind", kind)
				return false
			}

			for key, expectedValue := range map[string]string{
				"olm.operatorframework.io/owner-kind": "ClusterExtension",
				"olm.operatorframework.io/owner-name": sc.ClusterExtensionName,
			} {
				if labels[key] != expectedValue {
					shared.Log().V(1).Info("invalid resource label value", "name", obj.GetName(), "namespace", obj.GetNamespace(), "kind", kind, "label", key, "expected", expectedValue, "actual", labels["olm.operatorframework.io/owner-kind"])
					return false
				}
			}
			return true
		})
	}
	return nil
}

// ClusterExtensionResourcesRemoved waits for each previously gathered constituent resource to be deleted. Polls with timeout per resource.
func ClusterExtensionResourcesRemoved(ctx context.Context) error {
	sc := shared.Scenario(ctx)
	if len(sc.ExtensionObjects) == 0 {
		return fmt.Errorf("extension objects not found in context")
	}
	for _, obj := range sc.ExtensionObjects {
		if err := shared.ResourceEventuallyNotFound(ctx, fmt.Sprintf("%s/%s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())); err != nil {
			return err
		}
	}
	return nil
}

// ClusterExtensionVersionUpdate patches the ClusterExtension's catalog version to the specified value.
func ClusterExtensionVersionUpdate(ctx context.Context, version string) error {
	sc := shared.Scenario(ctx)
	patch := map[string]any{
		"spec": map[string]any{
			"source": map[string]any{
				"catalog": map[string]any{
					"version": version,
				},
			},
		},
	}
	pb, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = shared.K8sClient(ctx, "patch", "clusterextension", sc.ClusterExtensionName, "--type", "merge", "-p", string(pb))
	return err
}

// NamedClusterExtensionReportsCondition waits for a specific ClusterExtension (by name) to have a condition
// matching type and status. Polls with timeout.
func NamedClusterExtensionReportsCondition(ctx context.Context, extName, conditionType, conditionStatus string) error {
	sc := shared.Scenario(ctx)
	extName = shared.SubstituteScenarioVars(extName, sc)
	return shared.WaitForCondition(ctx, "clusterextension", extName, conditionType, metav1.ConditionStatus(conditionStatus), nil, nil)
}

// NamedClusterExtensionReportsConditionWithMessageFragment waits for a specific ClusterExtension (by name)
// to have a condition matching type, status, reason, with a message containing the specified fragment.
func NamedClusterExtensionReportsConditionWithMessageFragment(ctx context.Context, extName, conditionType, conditionStatus, conditionReason string, msgFragment *godog.DocString) error {
	extName = shared.SubstituteScenarioVars(extName, shared.Scenario(ctx))
	return shared.WaitForCondition(ctx, "clusterextension", extName, conditionType, metav1.ConditionStatus(conditionStatus), &conditionReason, shared.MessageFragmentComparison(ctx, msgFragment))
}

// OperatorTargetNamespace asserts that the operator deployment has the expected olm.targetNamespaces annotation.
func OperatorTargetNamespace(ctx context.Context, operator, namespace string) error {
	sc := shared.Scenario(ctx)
	operator = shared.SubstituteScenarioVars(operator, sc)
	namespace = shared.SubstituteScenarioVars(namespace, sc)
	raw, err := shared.K8sClient(ctx, "get", "deployment", "-n", sc.Namespace, operator, "-o", "json")
	if err != nil {
		return err
	}
	d := &appsv1.Deployment{}
	if err := json.Unmarshal([]byte(raw), d); err != nil {
		return err
	}

	tns := d.Spec.Template.Annotations["olm.targetNamespaces"]
	if tns != namespace {
		return fmt.Errorf("expected target namespace %s, got %s", namespace, tns)
	}

	if rec := shared.CommandRecorderFromContext(ctx); rec != nil {
		cmd := fmt.Sprintf("kubectl get deployment -n %s %s -o jsonpath='{.spec.template.metadata.annotations.olm\\.targetNamespaces}'", sc.Namespace, operator)
		rec.RecordCustom(cmd, tns+"\n", "")
	}
	return nil
}

// TrackCurrentClusterExtensionForCleanup saves the current ClusterExtension name in the cleanup list
// so it gets deleted at the end of the scenario. Call this before applying a second ClusterExtension
// in the same scenario, because ResourceIsApplied overwrites the tracked name.
func TrackCurrentClusterExtensionForCleanup(ctx context.Context) error {
	sc := shared.Scenario(ctx)
	if sc.ClusterExtensionName != "" {
		sc.ExtensionObjects = append(sc.ExtensionObjects, &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: sc.ClusterExtensionName},
		})
	}
	return nil
}

// collectHelmReleaseObjects returns a slice of client.Object containing the manifests in rel
func collectHelmReleaseObjects(rel *release.Release) ([]client.Object, error) {
	result := k8sresource.NewLocalBuilder().Flatten().Unstructured().Stream(strings.NewReader(rel.Manifest), rel.Name).Do()
	if err := result.Err(); err != nil {
		return nil, err
	}
	infos, err := result.Infos()
	if err != nil {
		return nil, err
	}

	objs := make([]client.Object, 0, len(infos))
	for _, info := range infos {
		clientObject, ok := info.Object.(client.Object)
		if !ok {
			return nil, fmt.Errorf("object of type %T does not implement client.Object", info.Object)
		}
		objs = append(objs, clientObject)
	}
	return objs, nil
}

// helmReleaseFromSecret returns the Helm Release object encoded in the secret. Note: this function does not yet support
// releases chunked over multiple Secrets
func helmReleaseFromSecret(secret *corev1.Secret) (*release.Release, error) {
	// OLM uses a custom release backend that compresses the release data
	gzReader, err := gzip.NewReader(strings.NewReader(string(secret.Data["chunk"])))
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	releaseJsonBytes, err := io.ReadAll(gzReader)
	if err != nil {
		return nil, err
	}

	var rel release.Release
	if err = json.Unmarshal(releaseJsonBytes, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// helmReleaseSecretForExtension returns the Helm release secret for the extension with name extName
func helmReleaseSecretForExtension(extName string) (*corev1.Secret, error) {
	out, err := shared.K8sClient(context.Background(), "get", "secrets", "-n", shared.OLMNamespace,
		"-l", fmt.Sprintf("name=%s,status=deployed", extName),
		"--field-selector", "type=operatorframework.io/index.v1", "-o", "json")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, err
	}

	var secretList corev1.SecretList
	if err = json.Unmarshal([]byte(out), &secretList); err != nil {
		return nil, err
	}
	if len(secretList.Items) != 1 {
		return nil, err
	}
	return &secretList.Items[0], nil
}

// latestActiveRevisionForExtension returns the latest active revision for the extension called extName
func latestActiveRevisionForExtension(extName string) (*ocv1.ClusterObjectSet, error) {
	revisions, err := shared.ListClusterObjectSetsInGroup(context.Background(), extName)
	if err != nil {
		return nil, fmt.Errorf("error listing revisions for extension '%s': %w", extName, err)
	}
	var latest *ocv1.ClusterObjectSet
	for i := range revisions {
		rev := &revisions[i]
		if rev.Spec.LifecycleState != ocv1.ClusterObjectSetLifecycleStateActive {
			continue
		}
		if latest == nil || rev.Spec.Revision > latest.Spec.Revision {
			latest = rev
		}
	}

	if latest == nil {
		return nil, fmt.Errorf("no active revisions found for extension '%s'", extName)
	}

	return latest, nil
}

// listExtensionResources returns a slice of client.Object containing all resources for a ClusterExtension
// this method is best called when the extension has been installed successfully. An error is returned if there was
// any issue in determining the extension's resources.
func listExtensionResources(extName string) ([]client.Object, error) {
	if enabled, found := shared.FeatureGates[features.BoxcutterRuntime]; found && enabled {
		return listExtensionRevisionResources(extName)
	}
	return listHelmReleaseResources(extName)
}

// listExtensionRevisionResources lists objects in the phases of the latest active revision
func listExtensionRevisionResources(extName string) ([]client.Object, error) {
	rev, err := latestActiveRevisionForExtension(extName)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest active revision for extension %s: %w", extName, err)
	}

	var objs []client.Object
	for i := range rev.Spec.Phases {
		phase := &rev.Spec.Phases[i]
		for j := range phase.Objects {
			specObj := &phase.Objects[j]
			switch specObj.Type {
			case ocv1.ObjectTypeSecretReference:
				resolved, err := shared.ResolveSecretRef(specObj.SecretRef)
				if err != nil {
					return nil, fmt.Errorf("resolving ref in phase %q object %d: %w", phase.Name, j, err)
				}
				objs = append(objs, resolved)
			case ocv1.ObjectTypeInlineManifest:
				objs = append(objs, &specObj.InlineManifest)
			default:
				return nil, fmt.Errorf("object %d in phase %q has unknown type %q", j, phase.Name, specObj.Type)
			}
		}
	}

	return objs, nil
}

// listHelmReleaseResources returns a slice of client.Object containing all resources for a ClusterExtension's
// Helm release. Note: The current implementation does not support release secrets chunked across multiple secrets
func listHelmReleaseResources(extName string) ([]client.Object, error) {
	secret, err := helmReleaseSecretForExtension(extName)
	if err != nil {
		return nil, fmt.Errorf("failed to get helm release secret for extension %s: %w", extName, err)
	}

	rel, err := helmReleaseFromSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get helm release from secret for cluster extension '%s': %w", extName, err)
	}

	objs, err := collectHelmReleaseObjects(rel)
	if err != nil {
		return nil, fmt.Errorf("failed to collect helm release objects for cluster extension '%s': %w", extName, err)
	}
	return objs, nil
}

func waitForExtensionCondition(ctx context.Context, conditionType string, conditionStatus metav1.ConditionStatus, conditionReason *string, msgCmp shared.MsgMatchFn) error {
	sc := shared.Scenario(ctx)
	return shared.WaitForCondition(ctx, "clusterextension", sc.ClusterExtensionName, conditionType, conditionStatus, conditionReason, msgCmp)
}

// NamespaceIsManagedByOLM waits for a namespace to be owned by the extension's active ClusterObjectSet.
func NamespaceIsManagedByOLM(ctx context.Context, nsName string) error {
	sc := shared.Scenario(ctx)
	nsName = shared.SubstituteScenarioVars(nsName, sc)

	shared.WaitFor(ctx, func() bool {
		revision, err := latestActiveRevisionForExtension(sc.ClusterExtensionName)
		if err != nil {
			shared.Log().V(1).Error(err, "failed to get active revision", "extension", sc.ClusterExtensionName)
			return false
		}
		out, err := shared.K8sClient(ctx, "get", "namespace", nsName, "-o", "json")
		if err != nil {
			return false
		}
		var ns corev1.Namespace
		if err := json.Unmarshal([]byte(out), &ns); err != nil {
			return false
		}
		owner := metav1.GetControllerOf(&ns)
		if owner == nil || owner.APIVersion != ocv1.GroupVersion.String() ||
			owner.Kind != ocv1.ClusterObjectSetKind || owner.Name != revision.Name || owner.UID != revision.UID {
			shared.Log().V(1).Info("Namespace is not yet owned by the extension's active revision",
				"namespace", nsName, "revision", revision.Name, "owner", owner)
			return false
		}
		return true
	})
	return nil
}

// gatherClusterExtensionObjects collects all resources related to the ClusterExtension container in
// either their Helm release Secret or ClusterObjectSet depending on the applier being used
// and saves them into the context.
func gatherClusterExtensionObjects(s *shared.World) error {
	objs, err := listExtensionResources(s.ClusterExtensionName)
	if err != nil {
		return fmt.Errorf("failed to load extension resources into context: %w", err)
	}
	s.ExtensionObjects = objs
	return nil
}

func TriggerClusterExtensionReconciliation(ctx context.Context) error {
	sc := shared.Scenario(ctx)

	out, err := shared.K8sClient(ctx, "get", "clusterextension", sc.ClusterExtensionName, "-o", "json")
	if err != nil {
		return fmt.Errorf("failed to get ClusterExtension %s: %w; stderr: %s", sc.ClusterExtensionName, err, shared.StderrOutput(err))
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return fmt.Errorf("failed to unmarshal ClusterExtension %s JSON: %w", sc.ClusterExtensionName, err)
	}

	currentEnforcement, _ := shared.NestedString(obj, "spec", "install", "preflight", "crdUpgradeSafety", "enforcement")

	newEnforcement := "None"
	if currentEnforcement == "None" {
		newEnforcement = "Strict"
	}

	payload := fmt.Sprintf(`{"spec":{"install":{"preflight":{"crdUpgradeSafety":{"enforcement":%q}}}}}`, newEnforcement)
	_, err = shared.K8sClient(ctx, "patch", "clusterextension", sc.ClusterExtensionName,
		"--type=merge",
		"-p", payload)
	if err != nil {
		return fmt.Errorf("failed to trigger reconciliation for ClusterExtension %s: %w; stderr: %s", sc.ClusterExtensionName, err, shared.StderrOutput(err))
	}
	return nil
}
