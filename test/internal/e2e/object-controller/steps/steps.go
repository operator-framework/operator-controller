package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

// RegisterSteps registers ClusterObjectSet and ClusterObjectDeployment assertions.
func RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" lifecycle is set to "([^"]+)"$`, ClusterObjectSetLifecycleUpdate)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+)$`, ClusterObjectSetReportsConditionWithoutMsg)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+) and Message:$`, ClusterObjectSetReportsConditionWithMsg)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+) and Message includes:$`, ClusterObjectSetReportsConditionWithMessageFragment)
	sc.Step(`^(?i)ClusterObjectDeployment "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+)$`, ClusterObjectDeploymentReportsConditionWithoutMsg)
	sc.Step(`^(?i)ClusterObjectDeployment "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+) and Message includes:$`, ClusterObjectDeploymentReportsConditionWithMessageFragment)
	sc.Step(`^(?i)ClusterObjectDeployment "([^"]+)" consistently reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+) for "([^"]+)"$`, ClusterObjectDeploymentConsistentlyReportsCondition)
	sc.Step(`^(?i)ClusterObjectSet is applied(?:\s+.*)?$`, shared.ResourceIsApplied)
	sc.Step(`^(?i)ClusterObjectSet with Namespace owner "([^"]*)" is applied$`, shared.ResourceWithNamespaceOwnerApplied)
	sc.Step(`^(?i)ClusterObjectDeployment with Namespace owner "([^"]*)" is applied$`, shared.ResourceWithNamespaceOwnerApplied)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" reconciliation is triggered$`, TriggerClusterObjectSetReconciliation)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" has observed phase "([^"]+)" with a non-empty digest$`, ClusterObjectSetHasObservedPhase)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" is archived$`, ClusterObjectSetIsArchived)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" is eventually deleted$`, ClusterObjectSetIsEventuallyDeleted)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" reports aggregate object counts: total=(\d+), present=(\d+), synced=(\d+), ready=(\d+)$`, ClusterObjectSetHasAggregateObjectCounts)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" contains annotation "([^"]+)" with value$`, ClusterObjectSetHasAnnotationWithValue)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" has label "([^"]+)" with value "([^"]+)"$`, ClusterObjectSetHasLabelWithValue)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" has group "([^"]+)"$`, ClusterObjectSetHasGroup)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" phase objects use SSA manager "([^"]+)"$`, ClusterObjectSetObjectsUseSSAManager)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets are remembered$`, RememberClusterObjectSetSecrets)
	sc.Step(`^(?i)the remembered revision secrets are removed$`, RememberedRevisionSecretsRemoved)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" phase objects are not found or not owned by the revision$`, ClusterObjectSetObjectsNotFoundOrNotOwned)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" phase objects are managed in Kubernetes secrets$`, ClusterObjectSetPhaseObjectsManagedInSecrets)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets exist in "([^"]+)" namespace$`, ClusterObjectSetReferredSecretsExist)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets are immutable$`, ClusterObjectSetReferredSecretsAreImmutable)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets contain labels$`, ClusterObjectSetReferredSecretsContainLabels)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets are owned by the object set$`, ClusterObjectSetReferredSecretsOwnedByObjectSet)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets are owned by ClusterExtension "([^"]+)"$`, ClusterObjectSetReferredSecretsOwnedByClusterExtension)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets have type "([^"]+)"$`, ClusterObjectSetReferredSecretsHaveType)
	sc.Step(`^(?i)ClusterObjectDeployment "([^"]+)" owns (\d+) ClusterObjectSets?$`, ClusterObjectDeploymentOwnsClusterObjectSets)
	sc.Step(`^(?i)ClusterObjectDeployment "([^"]+)" is owned by ClusterExtension "([^"]+)"$`, ClusterObjectDeploymentIsOwnedByClusterExtension)
}

type referredSecretRef struct {
	Name      string
	Namespace string
}

// ClusterObjectDeploymentConsistentlyReportsCondition fails if, over the given
// duration, the named ClusterObjectDeployment's condition ever deviates from the
// expected type/status/reason. Use it to prove a reason persists (e.g. that a hard
// stall keeps its reason past the progress deadline rather than flipping to
// ProgressDeadlineExceeded).
func ClusterObjectDeploymentConsistentlyReportsCondition(ctx context.Context, name, conditionType, conditionStatus, conditionReason, durationStr string) error {
	d, err := time.ParseDuration(durationStr)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", durationStr, err)
	}
	return shared.ConsistentlyCondition(ctx, "clusterobjectdeployment", shared.SubstituteScenarioVars(name, shared.Scenario(ctx)), conditionType, metav1.ConditionStatus(conditionStatus), conditionReason, d)
}

// ClusterObjectDeploymentIsOwnedByClusterExtension waits until the named ClusterObjectDeployment has
// the named ClusterExtension as its controller owner.
func ClusterObjectDeploymentIsOwnedByClusterExtension(ctx context.Context, codName, extName string) error {
	sc := shared.Scenario(ctx)
	codName = shared.SubstituteScenarioVars(codName, sc)
	extName = shared.SubstituteScenarioVars(extName, sc)
	shared.WaitFor(ctx, func() bool {
		cod, err := shared.GetResource("clusterobjectdeployment", codName, "")
		if err != nil {
			return false
		}
		ext, err := shared.GetResource("clusterextension", extName, "")
		if err != nil {
			return false
		}
		owner := metav1.GetControllerOf(cod)
		return owner != nil && owner.Kind == ocv1.ClusterExtensionKind && owner.Name == extName && owner.UID == ext.GetUID()
	})
	return nil
}

// ClusterObjectDeploymentOwnsClusterObjectSets waits until the named ClusterObjectDeployment is the
// controller owner of exactly expectedCount ClusterObjectSet revisions in its group.
func ClusterObjectDeploymentOwnsClusterObjectSets(ctx context.Context, codName string, expectedCount int) error {
	sc := shared.Scenario(ctx)
	codName = shared.SubstituteScenarioVars(codName, sc)
	shared.WaitFor(ctx, func() bool {
		revisions, err := shared.ListClusterObjectSetsInGroup(ctx, codName)
		if err != nil || len(revisions) != expectedCount {
			return false
		}
		cod, err := shared.GetResource("clusterobjectdeployment", codName, "")
		if err != nil {
			return false
		}
		for i := range revisions {
			owner := metav1.GetControllerOf(&revisions[i])
			if owner == nil || owner.Kind != ocv1.ClusterObjectDeploymentKind || owner.Name != codName || owner.UID != cod.GetUID() {
				return false
			}
		}
		return true
	})
	return nil
}

// ClusterObjectDeploymentReportsConditionWithMessageFragment waits for the named ClusterObjectDeployment to have a
// condition matching type, status, reason, with a message containing the specified fragment. Polls with timeout.
func ClusterObjectDeploymentReportsConditionWithMessageFragment(ctx context.Context, name, conditionType, conditionStatus, conditionReason string, msgFragment *godog.DocString) error {
	return shared.WaitForCondition(ctx, "clusterobjectdeployment", shared.SubstituteScenarioVars(name, shared.Scenario(ctx)), conditionType, metav1.ConditionStatus(conditionStatus), &conditionReason, shared.MessageFragmentComparison(ctx, msgFragment))
}

// ClusterObjectDeploymentReportsConditionWithoutMsg waits for the named ClusterObjectDeployment to have a
// condition matching type, status, and reason. Polls with timeout.
func ClusterObjectDeploymentReportsConditionWithoutMsg(ctx context.Context, name, conditionType, conditionStatus, conditionReason string) error {
	return shared.WaitForCondition(ctx, "clusterobjectdeployment", shared.SubstituteScenarioVars(name, shared.Scenario(ctx)), conditionType, metav1.ConditionStatus(conditionStatus), &conditionReason, nil)
}

// ClusterObjectSetHasAggregateObjectCounts waits for the named ClusterObjectSet to report exact
// aggregate object counts (summed across all phases). Polls with timeout.
func ClusterObjectSetHasAggregateObjectCounts(ctx context.Context, cosName string, total, present, synced, ready int64) error {
	sc := shared.Scenario(ctx)
	cosName = shared.SubstituteScenarioVars(cosName, sc)
	shared.WaitFor(ctx, func() bool {
		out, err := shared.K8sClient(ctx, "get", "clusterobjectset", cosName, "-o", "json")
		if err != nil {
			return false
		}
		var cos ocv1.ClusterObjectSet
		if err := json.Unmarshal([]byte(out), &cos); err != nil || shared.CurrentCondition(cos.Generation, cos.Status.Conditions, "Ready") == nil {
			return false
		}
		return ptr.Deref(cos.Status.ObjectCounts.Total, -1) == total &&
			ptr.Deref(cos.Status.ObjectCounts.Present, -1) == present &&
			ptr.Deref(cos.Status.ObjectCounts.Synced, -1) == synced &&
			ptr.Deref(cos.Status.ObjectCounts.Ready, -1) == ready
	})
	return nil
}

// ClusterObjectSetHasAnnotationWithValue waits for the named ClusterObjectSet to have the specified
// annotation with the expected value. Polls with timeout.
func ClusterObjectSetHasAnnotationWithValue(ctx context.Context, revisionName, annotationKey string, annotationValue *godog.DocString) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(strings.TrimSpace(revisionName), sc)
	expectedValue := ""
	if annotationValue != nil {
		expectedValue = annotationValue.Content
	}
	shared.WaitFor(ctx, func() bool {
		obj, err := shared.GetResource("clusterobjectset", revisionName, "")
		if err != nil {
			shared.Log().V(1).Error(err, "failed to get clusterobjectset", "name", revisionName)
			return false
		}
		if obj.GetAnnotations() == nil {
			return false
		}
		return obj.GetAnnotations()[annotationKey] == expectedValue
	})
	return nil
}

// ClusterObjectSetHasLabelWithValue waits for the named ClusterObjectSet to have the specified label
// with the expected value. Polls with timeout.
func ClusterObjectSetHasLabelWithValue(ctx context.Context, revisionName, labelKey, labelValue string) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(strings.TrimSpace(revisionName), sc)
	labelValue = shared.SubstituteScenarioVars(labelValue, sc)
	shared.WaitFor(ctx, func() bool {
		obj, err := shared.GetResource("clusterobjectset", revisionName, "")
		if err != nil {
			shared.Log().V(1).Error(err, "failed to get clusterobjectset", "name", revisionName)
			return false
		}
		if obj.GetLabels() == nil {
			return false
		}
		return obj.GetLabels()[labelKey] == labelValue
	})
	return nil
}

// ClusterObjectSetHasObservedPhase waits for the named ClusterObjectSet to have
// an observedPhases entry matching the given phase name with a non-empty digest. Polls with timeout.
func ClusterObjectSetHasObservedPhase(ctx context.Context, cosName, phaseName string) error {
	sc := shared.Scenario(ctx)
	cosName = shared.SubstituteScenarioVars(cosName, sc)
	phaseName = shared.SubstituteScenarioVars(phaseName, sc)

	shared.WaitFor(ctx, func() bool {
		out, err := shared.K8sClient(ctx, "get", "clusterobjectset", cosName, "-o", "json")
		if err != nil {
			return false
		}
		var cos ocv1.ClusterObjectSet
		if err := json.Unmarshal([]byte(out), &cos); err != nil || shared.CurrentCondition(cos.Generation, cos.Status.Conditions, "Ready") == nil {
			return false
		}
		for _, phase := range cos.Status.ObservedPhases {
			if phase.Name == phaseName {
				return phase.Digest != ""
			}
		}
		return false
	})
	return nil
}

// ClusterObjectSetIsArchived waits for the named ClusterObjectSet to have Ready=False
// with reason Archived. Polls with timeout.
func ClusterObjectSetIsArchived(ctx context.Context, revisionName string) error {
	return shared.WaitForCondition(ctx, "clusterobjectset", shared.SubstituteScenarioVars(revisionName, shared.Scenario(ctx)), "Ready", metav1.ConditionFalse, ptr.To("Archived"), nil)
}

// ClusterObjectSetIsEventuallyDeleted waits for the named ClusterObjectSet to be
// removed from the cluster, e.g. garbage collected once it is archived beyond the
// owning ClusterObjectDeployment's revisionHistoryLimit. Polls with timeout.
func ClusterObjectSetIsEventuallyDeleted(ctx context.Context, revisionName string) error {
	revisionName = shared.SubstituteScenarioVars(strings.TrimSpace(revisionName), shared.Scenario(ctx))
	shared.WaitFor(ctx, func() bool {
		out, err := shared.K8sClient(ctx, "get", "clusterobjectset", revisionName, "--ignore-not-found", "-o", "json")
		if err != nil {
			return false
		}
		return strings.TrimSpace(out) == ""
	})
	return nil
}

// ClusterObjectSetLifecycleUpdate patches the ClusterObjectSet's lifecycleState to the specified value.
func ClusterObjectSetLifecycleUpdate(ctx context.Context, cosName, lifecycle string) error {
	sc := shared.Scenario(ctx)
	cosName = shared.SubstituteScenarioVars(cosName, sc)
	patch := map[string]any{
		"spec": map[string]any{
			"lifecycleState": lifecycle,
		},
	}
	pb, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = shared.K8sClient(ctx, "patch", "clusterobjectset", cosName, "--type", "merge", "-p", string(pb))
	return err
}

// ClusterObjectSetObjectsNotFoundOrNotOwned waits for all objects described in the named
// ClusterObjectSet's phases to either not exist on the cluster or not contain the revision
// in their ownerReferences. Polls with timeout.
func ClusterObjectSetObjectsNotFoundOrNotOwned(ctx context.Context, revisionName string) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(strings.TrimSpace(revisionName), sc)

	// Get the ClusterObjectSet to extract its phase objects
	var rev ocv1.ClusterObjectSet
	shared.WaitFor(ctx, func() bool {
		out, err := shared.K8sClient(ctx, "get", "clusterobjectset", revisionName, "-o", "json")
		if err != nil {
			return false
		}
		return json.Unmarshal([]byte(out), &rev) == nil
	})

	// For each object in each phase, verify it either doesn't exist or
	// doesn't have the ClusterObjectSet in its ownerReferences
	for i, phase := range rev.Spec.Phases {
		for j, phaseObj := range phase.Objects {
			var obj *unstructured.Unstructured
			switch phaseObj.Type {
			case ocv1.ObjectTypeSecretReference:
				resolved, err := shared.ResolveSecretRef(phaseObj.SecretRef)
				if err != nil {
					return fmt.Errorf("resolving ref in phase %q object %d: %w", phase.Name, j, err)
				}
				obj = resolved
			case ocv1.ObjectTypeInlineManifest:
				obj = &phaseObj.InlineManifest
			default:
				return fmt.Errorf("clusterobjectset %q phase %d object %d has unknown type %q", revisionName, i, j, phaseObj.Type)
			}
			kind := obj.GetKind()
			name := obj.GetName()
			namespace := obj.GetNamespace()

			if kind == "" {
				return fmt.Errorf("clusterobjectset %q has a phase object with empty kind", revisionName)
			}
			if name == "" {
				return fmt.Errorf("clusterobjectset %q has a phase object with empty name (kind %q, namespace %q)", revisionName, kind, namespace)
			}

			shared.WaitFor(ctx, func() bool {
				args := []string{"get", kind, name, "--ignore-not-found", "-o", "json"}
				if namespace != "" {
					args = append(args, "-n", namespace)
				}
				out, err := shared.K8sClient(ctx, args...)
				if err != nil {
					return false
				}
				// If output is empty, the resource does not exist — condition satisfied
				if strings.TrimSpace(out) == "" {
					return true
				}
				clusterObj, err := shared.ToUnstructured(out)
				if err != nil {
					return false
				}

				// Check that no ownerReference points to this ClusterObjectSet
				for _, ref := range clusterObj.GetOwnerReferences() {
					if ref.Kind == ocv1.ClusterObjectSetKind && ref.Name == revisionName && ref.UID == rev.UID {
						shared.Log().V(1).Info("object still owned by revision",
							"kind", kind, "name", name, "namespace", namespace,
							"revision", revisionName)
						return false
					}
				}
				return true
			})
		}
	}
	return nil
}

// ClusterObjectSetPhaseObjectsManagedInSecrets verifies that every object in every phase of the named
// ClusterObjectSet uses a ref (not an inline object). Polls with timeout.
func ClusterObjectSetPhaseObjectsManagedInSecrets(ctx context.Context, revisionName string) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(strings.TrimSpace(revisionName), sc)

	shared.WaitFor(ctx, func() bool {
		obj, err := shared.GetResource("clusterobjectset", revisionName, "")
		if err != nil {
			return false
		}
		phases, ok, _ := unstructured.NestedSlice(obj.Object, "spec", "phases")
		if !ok || len(phases) == 0 {
			return false
		}
		for _, p := range phases {
			phase, ok := p.(map[string]interface{})
			if !ok {
				return false
			}
			objects, ok, _ := unstructured.NestedSlice(phase, "objects")
			if !ok || len(objects) == 0 {
				return false
			}
			for _, o := range objects {
				obj, ok := o.(map[string]interface{})
				if !ok {
					return false
				}
				ref, refOK, _ := unstructured.NestedMap(obj, "secretRef")
				if !refOK || len(ref) == 0 {
					shared.Log().V(1).Info("object does not use secretRef", "revision", revisionName)
					return false
				}
				name, _, _ := unstructured.NestedString(ref, "name")
				if name == "" {
					shared.Log().V(1).Info("ref has empty name", "revision", revisionName)
					return false
				}
			}
		}
		return true
	})
	return nil
}

// ClusterObjectSetReferredSecretsAreImmutable verifies that all referred Secrets for the named
// ClusterObjectSet are immutable. Polls with timeout.
func ClusterObjectSetReferredSecretsAreImmutable(ctx context.Context, revisionName string) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(strings.TrimSpace(revisionName), sc)

	secrets, err := listReferredSecrets(ctx, revisionName)
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		return fmt.Errorf("no referred secrets found for revision %q", revisionName)
	}
	for _, s := range secrets {
		if s.Immutable == nil || !*s.Immutable {
			return fmt.Errorf("referred secret %s/%s is not immutable", s.Namespace, s.Name)
		}
	}
	return nil
}

// ClusterObjectSetReferredSecretsContainLabels verifies that all referred Secrets for the named
// ClusterObjectSet have the expected labels specified in the data table. Polls with timeout.
func ClusterObjectSetReferredSecretsContainLabels(ctx context.Context, revisionName string, table *godog.Table) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(strings.TrimSpace(revisionName), sc)

	expected, err := shared.ParseKeyValueTable(table, sc)
	if err != nil {
		return fmt.Errorf("invalid labels table: %w", err)
	}

	shared.WaitFor(ctx, func() bool {
		secrets, err := listReferredSecrets(ctx, revisionName)
		if err != nil || len(secrets) == 0 {
			return false
		}
		for _, s := range secrets {
			if _, _, ok := shared.MatchLabels(s.Labels, expected); !ok {
				return false
			}
		}
		return true
	})
	return nil
}

// ClusterObjectSetReferredSecretsExist verifies that all Secrets referenced by the named
// ClusterObjectSet's phase objects exist in the given namespace. Polls with timeout.
func ClusterObjectSetReferredSecretsExist(ctx context.Context, revisionName, namespace string) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(strings.TrimSpace(revisionName), sc)
	namespace = shared.SubstituteScenarioVars(strings.TrimSpace(namespace), sc)

	secretNames, err := CollectReferredSecretNames(ctx, revisionName)
	if err != nil {
		return err
	}

	for _, name := range secretNames {
		shared.WaitFor(ctx, func() bool {
			_, err := shared.GetResource("secret", name, namespace)
			return err == nil
		})
	}
	return nil
}

// ClusterObjectSetReferredSecretsHaveType verifies that all referred Secrets for the named
// ClusterObjectSet have the specified Secret type.
func ClusterObjectSetReferredSecretsHaveType(ctx context.Context, revisionName, expectedType string) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(strings.TrimSpace(revisionName), sc)

	secrets, err := listReferredSecrets(ctx, revisionName)
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		return fmt.Errorf("no referred secrets found for revision %q", revisionName)
	}

	for _, s := range secrets {
		if string(s.Type) != expectedType {
			return fmt.Errorf("secret %s/%s has type %q, expected %q", s.Namespace, s.Name, s.Type, expectedType)
		}
	}
	return nil
}

// ClusterObjectSetReferredSecretsOwnedByClusterExtension verifies that every referred Secret of the
// named revision is controller-owned by the named ClusterExtension. On the current operator-controller
// path the ClusterExtension owns the content Secrets directly (so deleting it garbage-collects them).
func ClusterObjectSetReferredSecretsOwnedByClusterExtension(ctx context.Context, revisionName, extName string) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(strings.TrimSpace(revisionName), sc)
	extName = shared.SubstituteScenarioVars(strings.TrimSpace(extName), sc)

	ext, err := shared.GetResource("clusterextension", extName, "")
	if err != nil {
		return fmt.Errorf("getting ClusterExtension %q: %w", extName, err)
	}

	secrets, err := listReferredSecrets(ctx, revisionName)
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		return fmt.Errorf("no referred secrets found for revision %q", revisionName)
	}

	for _, s := range secrets {
		found := false
		for _, ref := range s.OwnerReferences {
			if ref.Kind == ocv1.ClusterExtensionKind && ref.Name == extName && ref.UID == ext.GetUID() {
				if ref.Controller == nil || !*ref.Controller {
					return fmt.Errorf("secret %s/%s has ownerReference to ClusterExtension but controller is not true", s.Namespace, s.Name)
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("secret %s/%s is not owned by ClusterExtension %q (uid %s)", s.Namespace, s.Name, extName, ext.GetUID())
		}
	}
	return nil
}

// ClusterObjectSetReferredSecretsOwnedByObjectSet verifies that all referred Secrets for the named
// ClusterObjectSet have an ownerReference pointing to the ClusterObjectSet with controller=true.
func ClusterObjectSetReferredSecretsOwnedByObjectSet(ctx context.Context, revisionName string) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(strings.TrimSpace(revisionName), sc)

	cosObj, err := shared.GetResource("clusterobjectset", revisionName, "")
	if err != nil {
		return fmt.Errorf("getting ClusterObjectSet %q: %w", revisionName, err)
	}
	cosUID := cosObj.GetUID()

	secrets, err := listReferredSecrets(ctx, revisionName)
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		return fmt.Errorf("no referred secrets found for revision %q", revisionName)
	}

	for _, s := range secrets {
		found := false
		for _, ref := range s.OwnerReferences {
			if ref.Kind == ocv1.ClusterObjectSetKind && ref.Name == revisionName && ref.UID == cosUID {
				if ref.Controller == nil || !*ref.Controller {
					return fmt.Errorf("secret %s/%s has ownerReference to ClusterObjectSet but controller is not true", s.Namespace, s.Name)
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("secret %s/%s does not have ownerReference to ClusterObjectSet %q (uid %s)", s.Namespace, s.Name, revisionName, cosUID)
		}
	}
	return nil
}

// ClusterObjectSetReportsConditionWithMessageFragment waits for the named ClusterObjectSet to have a condition
// matching type, status, reason, with a message containing the specified fragment. Polls with timeout.
func ClusterObjectSetReportsConditionWithMessageFragment(ctx context.Context, revisionName, conditionType, conditionStatus, conditionReason string, msgFragment *godog.DocString) error {
	return shared.WaitForCondition(ctx, "clusterobjectset", shared.SubstituteScenarioVars(revisionName, shared.Scenario(ctx)), conditionType, metav1.ConditionStatus(conditionStatus), &conditionReason, shared.MessageFragmentComparison(ctx, msgFragment))
}

// ClusterObjectSetReportsConditionWithMsg waits for the named ClusterObjectSet to have a condition
// matching type, status, reason, and message. Polls with timeout.
func ClusterObjectSetReportsConditionWithMsg(ctx context.Context, revisionName, conditionType, conditionStatus, conditionReason string, msg *godog.DocString) error {
	return shared.WaitForCondition(ctx, "clusterobjectset", shared.SubstituteScenarioVars(revisionName, shared.Scenario(ctx)), conditionType, metav1.ConditionStatus(conditionStatus), &conditionReason, shared.MessageComparison(ctx, msg))
}

// ClusterObjectSetReportsConditionWithoutMsg waits for the named ClusterObjectSet to have a condition
// matching type, status, and reason. Polls with timeout.
func ClusterObjectSetReportsConditionWithoutMsg(ctx context.Context, revisionName, conditionType, conditionStatus, conditionReason string) error {
	return shared.WaitForCondition(ctx, "clusterobjectset", shared.SubstituteScenarioVars(revisionName, shared.Scenario(ctx)), conditionType, metav1.ConditionStatus(conditionStatus), &conditionReason, nil)
}

// CollectReferredSecretNames returns the unique set of Secret names referenced by the ClusterObjectSet's phase objects.
func CollectReferredSecretNames(ctx context.Context, revisionName string) ([]string, error) {
	var names []string
	seen := sets.New[string]()

	var obj *unstructured.Unstructured
	shared.WaitFor(ctx, func() bool {
		var err error
		obj, err = shared.GetResource("clusterobjectset", revisionName, "")
		return err == nil
	})

	phases, _, _ := unstructured.NestedSlice(obj.Object, "spec", "phases")
	for _, p := range phases {
		phase, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		objects, _, _ := unstructured.NestedSlice(phase, "objects")
		for _, o := range objects {
			phaseObj, ok := o.(map[string]interface{})
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(phaseObj, "secretRef", "name")
			if name != "" && !seen.Has(name) {
				seen.Insert(name)
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no referred secret names found in ClusterObjectSet %q", revisionName)
	}
	return names, nil
}

// TriggerClusterObjectSetReconciliation annotates the named ClusterObjectSet
// to trigger a new reconciliation cycle.
func TriggerClusterObjectSetReconciliation(ctx context.Context, cosName string) error {
	sc := shared.Scenario(ctx)
	cosName = shared.SubstituteScenarioVars(cosName, sc)
	_, err := shared.K8sClient(ctx, "annotate", "clusterobjectset", cosName, "--overwrite",
		fmt.Sprintf("e2e-trigger=%d", time.Now().UnixNano()))
	return err
}

// collectReferredSecretRefs returns the unique Secrets (name + namespace) referenced by the
// ClusterObjectSet's phase objects. An empty ref namespace defaults to the OLM system namespace.
func collectReferredSecretRefs(ctx context.Context, revisionName string) ([]referredSecretRef, error) {
	var refs []referredSecretRef
	seen := sets.New[string]()

	var obj *unstructured.Unstructured
	shared.WaitFor(ctx, func() bool {
		var err error
		obj, err = shared.GetResource("clusterobjectset", revisionName, "")
		return err == nil
	})

	phases, _, _ := unstructured.NestedSlice(obj.Object, "spec", "phases")
	for _, p := range phases {
		phase, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		objects, _, _ := unstructured.NestedSlice(phase, "objects")
		for _, o := range objects {
			phaseObj, ok := o.(map[string]interface{})
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(phaseObj, "secretRef", "name")
			if name == "" {
				continue
			}
			ns, _, _ := unstructured.NestedString(phaseObj, "secretRef", "namespace")
			if ns == "" {
				ns = shared.OLMNamespace
			}
			key := ns + "/" + name
			if seen.Has(key) {
				continue
			}
			seen.Insert(key)
			refs = append(refs, referredSecretRef{Name: name, Namespace: ns})
		}
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no referred secret refs found in ClusterObjectSet %q", revisionName)
	}
	return refs, nil
}

// listReferredSecrets returns the Secrets referenced by the named ClusterObjectSet's phase refs.
// Discovery is driven off the ClusterObjectSet spec rather than a label, so it is robust to how the
// Secrets are named or labeled.
func listReferredSecrets(ctx context.Context, revisionName string) ([]corev1.Secret, error) {
	refs, err := collectReferredSecretRefs(ctx, revisionName)
	if err != nil {
		return nil, err
	}
	secrets := make([]corev1.Secret, 0, len(refs))
	for _, r := range refs {
		out, err := shared.K8sClient(ctx, "get", "secret", r.Name, "-n", r.Namespace, "-o", "json")
		if err != nil {
			return nil, fmt.Errorf("getting referred secret %s/%s: %w", r.Namespace, r.Name, err)
		}
		var s corev1.Secret
		if err := json.Unmarshal([]byte(out), &s); err != nil {
			return nil, fmt.Errorf("unmarshalling secret %s/%s: %w", r.Namespace, r.Name, err)
		}
		secrets = append(secrets, s)
	}
	return secrets, nil
}

func ClusterObjectSetHasGroup(ctx context.Context, revisionName, group string) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(revisionName, sc)
	group = shared.SubstituteScenarioVars(group, sc)
	shared.WaitFor(ctx, func() bool {
		obj, err := shared.GetResource("clusterobjectset", revisionName, "")
		if err != nil {
			return false
		}
		var cos ocv1.ClusterObjectSet
		return runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &cos) == nil && cos.Spec.Group == group
	})
	return nil
}

// ClusterObjectSetObjectsUseSSAManager checks actual managed fields on every
// inline or externally stored phase object, including their revision owner.
func ClusterObjectSetObjectsUseSSAManager(ctx context.Context, revisionName, manager string) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(revisionName, sc)
	manager = shared.SubstituteScenarioVars(manager, sc)
	shared.WaitFor(ctx, func() bool {
		obj, err := shared.GetResource("clusterobjectset", revisionName, "")
		if err != nil {
			return false
		}
		var cos ocv1.ClusterObjectSet
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &cos); err != nil {
			return false
		}
		count := 0
		for _, phase := range cos.Spec.Phases {
			for _, phaseObj := range phase.Objects {
				desired := &phaseObj.InlineManifest
				if phaseObj.Type == ocv1.ObjectTypeSecretReference {
					desired, err = shared.ResolveSecretRef(phaseObj.SecretRef)
					if err != nil {
						return false
					}
				}
				// kubectl's JSON/YAML printers omit managedFields by default.
				args := []string{"get", desired.GetKind(), desired.GetName(), "-o", "json", "--show-managed-fields"}
				if desired.GetNamespace() != "" {
					args = append(args, "-n", desired.GetNamespace())
				}
				out, err := shared.K8sClient(ctx, args...)
				if err != nil {
					return false
				}
				actual := &unstructured.Unstructured{}
				if err := json.Unmarshal([]byte(out), &actual.Object); err != nil {
					return false
				}
				owner := metav1.GetControllerOf(actual)
				if owner == nil || owner.Kind != ocv1.ClusterObjectSetKind || owner.UID != cos.UID {
					return false
				}
				found := false
				for _, fields := range actual.GetManagedFields() {
					if fields.Operation != metav1.ManagedFieldsOperationApply {
						continue
					}
					if strings.HasPrefix(fields.Manager, "cos-group/") && fields.Manager != manager {
						return false
					}
					if fields.Manager == manager {
						found = true
					}
				}
				if !found {
					return false
				}
				count++
			}
		}
		return count > 0
	})
	return nil
}

func RememberClusterObjectSetSecrets(ctx context.Context, revisionName string) error {
	sc := shared.Scenario(ctx)
	revisionName = shared.SubstituteScenarioVars(revisionName, sc)
	names, err := CollectReferredSecretNames(ctx, revisionName)
	if err != nil {
		return err
	}
	for _, name := range names {
		sc.RevisionSecrets = append(sc.RevisionSecrets, shared.Resource{Kind: "secret", Name: name, Namespace: shared.OLMNamespace})
	}
	return nil
}

func RememberedRevisionSecretsRemoved(ctx context.Context) error {
	sc := shared.Scenario(ctx)
	if len(sc.RevisionSecrets) == 0 {
		return fmt.Errorf("no revision secrets were remembered")
	}
	for _, secret := range sc.RevisionSecrets {
		shared.WaitFor(ctx, func() bool {
			out, err := shared.K8sClient(ctx, "get", secret.Kind, secret.Name, "-n", secret.Namespace, "--ignore-not-found", "-o", "name")
			return err == nil && strings.TrimSpace(out) == ""
		})
	}
	return nil
}
