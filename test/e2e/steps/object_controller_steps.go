package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/component-base/featuregate"
	"k8s.io/utils/ptr"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

const (
	objectControllerFeature        featuregate.Feature = "ObjectController"
	objectControllerDeploymentName string              = "object-controller-controller-manager"
)

// ObjectControllerIsAvailable verifies the independently deployed controller.
// Explicit standalone runs also reject ClusterExtension and ClusterCatalog APIs.
func ObjectControllerIsAvailable(ctx context.Context) error {
	namespace, found := componentNamespaces[objectControllerComponent]
	if !found || namespace == "" {
		return fmt.Errorf("object-controller is not installed: Deployment %q was not discovered", objectControllerDeploymentName)
	}
	if objectControllerOnly {
		out, err := k8sClient(ctx, "get", "crd", "clusterextensions.olm.operatorframework.io", "clustercatalogs.olm.operatorframework.io", "--ignore-not-found", "-o", "name")
		if err != nil {
			return fmt.Errorf("checking standalone API isolation: %w", err)
		}
		if strings.TrimSpace(out) != "" {
			return fmt.Errorf("standalone object-controller tests require ClusterExtension and ClusterCatalog APIs to be absent; found: %s", strings.TrimSpace(out))
		}
	}
	if _, err := k8sClient(ctx, "get", "crd", "clusterobjectsets.olm.operatorframework.io"); err != nil {
		return fmt.Errorf("ClusterObjectSet API is unavailable: %w", err)
	}
	_, err := k8sClient(ctx, "rollout", "status", "deployment/"+objectControllerDeploymentName,
		"-n", namespace, "--timeout="+timeout.String())
	if err != nil {
		return fmt.Errorf("object-controller is unavailable: %w", err)
	}
	return nil
}

// RegisterObjectControllerSteps registers ClusterObjectSet API operations and assertions.
// ClusterExtension scenarios can reuse these assertions without defining the
// standalone suite's availability or feature-gate requirements.
func RegisterObjectControllerSteps(sc *godog.ScenarioContext) {
	sc.Step(`^object-controller is available$`, ObjectControllerIsAvailable)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" lifecycle is set to "([^"]+)"$`, ClusterObjectSetLifecycleUpdate)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+)$`, ClusterObjectSetReportsConditionWithoutMsg)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+) and Message:$`, ClusterObjectSetReportsConditionWithMsg)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+) and Message includes:$`, ClusterObjectSetReportsConditionWithMessageFragment)
	sc.Step(`^(?i)ClusterObjectSet is applied(?:\s+.*)?$`, ResourceIsApplied)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" reconciliation is triggered$`, TriggerClusterObjectSetReconciliation)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" has observed phase "([^"]+)" with a non-empty digest$`, ClusterObjectSetHasObservedPhase)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" is archived$`, ClusterObjectSetIsArchived)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" reports phase "([^"]+)" object counts: total=(\d+), present=(\d+), synced=(\d+), ready=(\d+)$`, ClusterObjectSetPhaseHasObjectCounts)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" reports aggregate object counts: total=(\d+), present=(\d+), synced=(\d+), ready=(\d+)$`, ClusterObjectSetHasAggregateObjectCounts)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" contains annotation "([^"]+)" with value$`, ClusterObjectSetHasAnnotationWithValue)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" has label "([^"]+)" with value "([^"]+)"$`, ClusterObjectSetHasLabelWithValue)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" phase objects are not found or not owned by the revision$`, ClusterObjectSetObjectsNotFoundOrNotOwned)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" phase objects are managed in Kubernetes secrets$`, ClusterObjectSetPhaseObjectsManagedInSecrets)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets exist in "([^"]+)" namespace$`, ClusterObjectSetReferredSecretsExist)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets are immutable$`, ClusterObjectSetReferredSecretsAreImmutable)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets contain labels$`, ClusterObjectSetReferredSecretsContainLabels)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets are owned by the object set$`, ClusterObjectSetReferredSecretsOwnedByObjectSet)
	sc.Step(`^(?i)ClusterObjectSet "([^"]+)" referred secrets have type "([^"]+)"$`, ClusterObjectSetReferredSecretsHaveType)
}

// ClusterObjectSetLifecycleUpdate patches the ClusterObjectSet's lifecycleState to the specified value.
func ClusterObjectSetLifecycleUpdate(ctx context.Context, cosName, lifecycle string) error {
	sc := scenarioCtx(ctx)
	cosName = substituteScenarioVars(cosName, sc)
	patch := map[string]any{
		"spec": map[string]any{
			"lifecycleState": lifecycle,
		},
	}
	pb, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = k8sClient(ctx, "patch", "clusterobjectset", cosName, "--type", "merge", "-p", string(pb))
	return err
}

// ClusterObjectSetReportsConditionWithoutMsg waits for the named ClusterObjectSet to have a condition
// matching type, status, and reason. Polls with timeout.
func ClusterObjectSetReportsConditionWithoutMsg(ctx context.Context, revisionName, conditionType, conditionStatus, conditionReason string) error {
	return waitForCondition(ctx, "clusterobjectset", substituteScenarioVars(revisionName, scenarioCtx(ctx)), conditionType, conditionStatus, &conditionReason, nil)
}

// ClusterObjectSetReportsConditionWithMsg waits for the named ClusterObjectSet to have a condition
// matching type, status, reason, and message. Polls with timeout.
func ClusterObjectSetReportsConditionWithMsg(ctx context.Context, revisionName, conditionType, conditionStatus, conditionReason string, msg *godog.DocString) error {
	return waitForCondition(ctx, "clusterobjectset", substituteScenarioVars(revisionName, scenarioCtx(ctx)), conditionType, conditionStatus, &conditionReason, messageComparison(ctx, msg))
}

// ClusterObjectSetReportsConditionWithMessageFragment waits for the named ClusterObjectSet to have a condition
// matching type, status, reason, with a message containing the specified fragment. Polls with timeout.
func ClusterObjectSetReportsConditionWithMessageFragment(ctx context.Context, revisionName, conditionType, conditionStatus, conditionReason string, msgFragment *godog.DocString) error {
	return waitForCondition(ctx, "clusterobjectset", substituteScenarioVars(revisionName, scenarioCtx(ctx)), conditionType, conditionStatus, &conditionReason, messageFragmentComparison(ctx, msgFragment))
}

// TriggerClusterObjectSetReconciliation annotates the named ClusterObjectSet
// to trigger a new reconciliation cycle.
func TriggerClusterObjectSetReconciliation(ctx context.Context, cosName string) error {
	sc := scenarioCtx(ctx)
	cosName = substituteScenarioVars(cosName, sc)
	_, err := k8sClient(ctx, "annotate", "clusterobjectset", cosName, "--overwrite",
		fmt.Sprintf("e2e-trigger=%d", time.Now().UnixNano()))
	return err
}

// ClusterObjectSetHasObservedPhase waits for the named ClusterObjectSet to have
// an observedPhases entry matching the given phase name with a non-empty digest. Polls with timeout.
func ClusterObjectSetHasObservedPhase(ctx context.Context, cosName, phaseName string) error {
	sc := scenarioCtx(ctx)
	cosName = substituteScenarioVars(cosName, sc)
	phaseName = substituteScenarioVars(phaseName, sc)

	waitFor(ctx, func() bool {
		out, err := k8sClient(ctx, "get", "clusterobjectset", cosName, "-o",
			fmt.Sprintf(`jsonpath={.status.observedPhases[?(@.name=="%s")].digest}`, phaseName))
		if err != nil {
			return false
		}
		return strings.TrimSpace(out) != ""
	})
	return nil
}

// ClusterObjectSetIsArchived waits for the named ClusterObjectSet to have Ready=False
// with reason Archived. Polls with timeout.
func ClusterObjectSetIsArchived(ctx context.Context, revisionName string) error {
	return waitForCondition(ctx, "clusterobjectset", substituteScenarioVars(revisionName, scenarioCtx(ctx)), "Ready", "False", ptr.To("Archived"), nil)
}

// ClusterObjectSetPhaseHasObjectCounts waits for a specific phase of the named ClusterObjectSet to
// report exact object counts for total, present, synced, and available. Polls with timeout.
func ClusterObjectSetPhaseHasObjectCounts(ctx context.Context, cosName, phaseName string, total, present, synced, ready int64) error {
	sc := scenarioCtx(ctx)
	cosName = substituteScenarioVars(cosName, sc)
	phaseName = substituteScenarioVars(phaseName, sc)
	waitFor(ctx, func() bool {
		out, err := k8sClient(ctx, "get", "clusterobjectset", cosName, "-o", "json")
		if err != nil {
			return false
		}
		var cos ocv1.ClusterObjectSet
		if err := json.Unmarshal([]byte(out), &cos); err != nil {
			return false
		}
		for _, phase := range cos.Status.ObservedPhases {
			if phase.Name == phaseName {
				return phase.ObjectCounts.Total == total &&
					phase.ObjectCounts.Present == present &&
					phase.ObjectCounts.Synced == synced &&
					phase.ObjectCounts.Ready == ready
			}
		}
		return false
	})
	return nil
}

// ClusterObjectSetHasAggregateObjectCounts waits for the named ClusterObjectSet to report exact
// aggregate object counts (summed across all phases). Polls with timeout.
func ClusterObjectSetHasAggregateObjectCounts(ctx context.Context, cosName string, total, present, synced, ready int64) error {
	sc := scenarioCtx(ctx)
	cosName = substituteScenarioVars(cosName, sc)
	waitFor(ctx, func() bool {
		out, err := k8sClient(ctx, "get", "clusterobjectset", cosName, "-o", "json")
		if err != nil {
			return false
		}
		var cos ocv1.ClusterObjectSet
		if err := json.Unmarshal([]byte(out), &cos); err != nil {
			return false
		}
		return cos.Status.ObjectCounts.Total == total &&
			cos.Status.ObjectCounts.Present == present &&
			cos.Status.ObjectCounts.Synced == synced &&
			cos.Status.ObjectCounts.Ready == ready
	})
	return nil
}

// ClusterObjectSetHasAnnotationWithValue waits for the named ClusterObjectSet to have the specified
// annotation with the expected value. Polls with timeout.
func ClusterObjectSetHasAnnotationWithValue(ctx context.Context, revisionName, annotationKey string, annotationValue *godog.DocString) error {
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(strings.TrimSpace(revisionName), sc)
	expectedValue := ""
	if annotationValue != nil {
		expectedValue = annotationValue.Content
	}
	waitFor(ctx, func() bool {
		obj, err := getResource("clusterobjectset", revisionName, "")
		if err != nil {
			logger.V(1).Error(err, "failed to get clusterobjectset", "name", revisionName)
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
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(strings.TrimSpace(revisionName), sc)
	labelValue = substituteScenarioVars(labelValue, sc)
	waitFor(ctx, func() bool {
		obj, err := getResource("clusterobjectset", revisionName, "")
		if err != nil {
			logger.V(1).Error(err, "failed to get clusterobjectset", "name", revisionName)
			return false
		}
		if obj.GetLabels() == nil {
			return false
		}
		return obj.GetLabels()[labelKey] == labelValue
	})
	return nil
}

// ClusterObjectSetObjectsNotFoundOrNotOwned waits for all objects described in the named
// ClusterObjectSet's phases to either not exist on the cluster or not contain the revision
// in their ownerReferences. Polls with timeout.
func ClusterObjectSetObjectsNotFoundOrNotOwned(ctx context.Context, revisionName string) error {
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(strings.TrimSpace(revisionName), sc)

	// Get the ClusterObjectSet to extract its phase objects
	var rev ocv1.ClusterObjectSet
	waitFor(ctx, func() bool {
		out, err := k8sClient(ctx, "get", "clusterobjectset", revisionName, "-o", "json")
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
			switch {
			case phaseObj.Ref.Name != "":
				resolved, err := resolveObjectRef(phaseObj.Ref)
				if err != nil {
					return fmt.Errorf("resolving ref in phase %q object %d: %w", phase.Name, j, err)
				}
				obj = resolved
			case len(phaseObj.Object.Object) > 0:
				obj = &phaseObj.Object
			default:
				return fmt.Errorf("clusterobjectset %q phase %d object %d has neither ref nor inline object", revisionName, i, j)
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

			waitFor(ctx, func() bool {
				args := []string{"get", kind, name, "--ignore-not-found", "-o", "json"}
				if namespace != "" {
					args = append(args, "-n", namespace)
				}
				out, err := k8sClient(ctx, args...)
				if err != nil {
					return false
				}
				// If output is empty, the resource does not exist — condition satisfied
				if strings.TrimSpace(out) == "" {
					return true
				}
				clusterObj, err := toUnstructured(out)
				if err != nil {
					return false
				}

				// Check that no ownerReference points to this ClusterObjectSet
				for _, ref := range clusterObj.GetOwnerReferences() {
					if ref.Kind == ocv1.ClusterObjectSetKind && ref.Name == revisionName && ref.UID == rev.UID {
						logger.V(1).Info("object still owned by revision",
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
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(strings.TrimSpace(revisionName), sc)

	waitFor(ctx, func() bool {
		obj, err := getResource("clusterobjectset", revisionName, "")
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
				ref, refOK, _ := unstructured.NestedMap(obj, "ref")
				if !refOK || len(ref) == 0 {
					logger.V(1).Info("object does not use ref", "revision", revisionName)
					return false
				}
				name, _, _ := unstructured.NestedString(ref, "name")
				if name == "" {
					logger.V(1).Info("ref has empty name", "revision", revisionName)
					return false
				}
			}
		}
		return true
	})
	return nil
}

// ClusterObjectSetReferredSecretsExist verifies that all Secrets referenced by the named
// ClusterObjectSet's phase objects exist in the given namespace. Polls with timeout.
func ClusterObjectSetReferredSecretsExist(ctx context.Context, revisionName, namespace string) error {
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(strings.TrimSpace(revisionName), sc)
	namespace = substituteScenarioVars(strings.TrimSpace(namespace), sc)

	secretNames, err := collectReferredSecretNames(ctx, revisionName)
	if err != nil {
		return err
	}

	for _, name := range secretNames {
		waitFor(ctx, func() bool {
			_, err := getResource("secret", name, namespace)
			return err == nil
		})
	}
	return nil
}

// ClusterObjectSetReferredSecretsAreImmutable verifies that all referred Secrets for the named
// ClusterObjectSet are immutable. Polls with timeout.
func ClusterObjectSetReferredSecretsAreImmutable(ctx context.Context, revisionName string) error {
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(strings.TrimSpace(revisionName), sc)

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
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(strings.TrimSpace(revisionName), sc)

	expected, err := parseKeyValueTable(table, sc)
	if err != nil {
		return fmt.Errorf("invalid labels table: %w", err)
	}

	waitFor(ctx, func() bool {
		secrets, err := listReferredSecrets(ctx, revisionName)
		if err != nil || len(secrets) == 0 {
			return false
		}
		for _, s := range secrets {
			if _, _, ok := matchLabels(s.Labels, expected); !ok {
				return false
			}
		}
		return true
	})
	return nil
}

// ClusterObjectSetReferredSecretsOwnedByObjectSet verifies that all referred Secrets for the named
// ClusterObjectSet have an ownerReference pointing to the ClusterObjectSet with controller=true.
func ClusterObjectSetReferredSecretsOwnedByObjectSet(ctx context.Context, revisionName string) error {
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(strings.TrimSpace(revisionName), sc)

	cosObj, err := getResource("clusterobjectset", revisionName, "")
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

// ClusterObjectSetReferredSecretsHaveType verifies that all referred Secrets for the named
// ClusterObjectSet have the specified Secret type.
func ClusterObjectSetReferredSecretsHaveType(ctx context.Context, revisionName, expectedType string) error {
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(strings.TrimSpace(revisionName), sc)

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

// collectReferredSecretNames returns the unique set of Secret names referenced by the ClusterObjectSet's phase objects.
func collectReferredSecretNames(ctx context.Context, revisionName string) ([]string, error) {
	var names []string
	seen := sets.New[string]()

	var obj *unstructured.Unstructured
	waitFor(ctx, func() bool {
		var err error
		obj, err = getResource("clusterobjectset", revisionName, "")
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
			name, _, _ := unstructured.NestedString(phaseObj, "ref", "name")
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

// listReferredSecrets fetches each unique Secret referenced by the named
// ClusterObjectSet's phase objects, defaulting an omitted ref namespace to the OLM namespace.
func listReferredSecrets(ctx context.Context, revisionName string) ([]corev1.Secret, error) {
	out, err := k8sClient(ctx, "get", "clusterobjectset", revisionName, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("getting ClusterObjectSet %q: %w", revisionName, err)
	}
	var revision ocv1.ClusterObjectSet
	if err := json.Unmarshal([]byte(out), &revision); err != nil {
		return nil, fmt.Errorf("unmarshalling ClusterObjectSet %q: %w", revisionName, err)
	}
	var secrets []corev1.Secret
	seen := sets.New[types.NamespacedName]()
	for _, phase := range revision.Spec.Phases {
		for _, object := range phase.Objects {
			ref := object.Ref
			if ref.Name == "" {
				continue
			}
			if ref.Namespace == "" {
				ref.Namespace = olmNamespace
			}
			key := types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}
			if seen.Has(key) {
				continue
			}
			seen.Insert(key)
			out, err := k8sClient(ctx, "get", "secret", ref.Name, "-n", ref.Namespace, "-o", "json")
			if err != nil {
				return nil, fmt.Errorf("getting referred Secret %s: %w", key, err)
			}
			var secret corev1.Secret
			if err := json.Unmarshal([]byte(out), &secret); err != nil {
				return nil, fmt.Errorf("unmarshalling referred Secret %s: %w", key, err)
			}
			secrets = append(secrets, secret)
		}
	}
	return secrets, nil
}
