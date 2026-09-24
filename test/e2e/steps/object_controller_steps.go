package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"k8s.io/component-base/featuregate"
	"k8s.io/utils/ptr"
)

const (
	objectControllerFeature        featuregate.Feature = "ObjectController"
	objectControllerDeploymentName string              = "object-controller-controller-manager"
)

// ObjectControllerIsAvailable verifies the independently deployed controller.
// Explicit standalone runs also reject ClusterExtension and ClusterCatalog APIs.
func ObjectControllerIsAvailable(ctx context.Context) error {
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
		"-n", namespaceForComponent("object-controller"), "--timeout="+timeout.String())
	if err != nil {
		return fmt.Errorf("object-controller is unavailable: %w", err)
	}
	return nil
}

// RegisterObjectControllerSteps registers direct ClusterObjectSet API operations.
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

// ClusterObjectSetIsArchived waits for the named ClusterObjectSet to have Progressing=False
// with reason Archived. Polls with timeout.
func ClusterObjectSetIsArchived(ctx context.Context, revisionName string) error {
	return waitForCondition(ctx, "clusterobjectset", substituteScenarioVars(revisionName, scenarioCtx(ctx)), "Progressing", "False", ptr.To("Archived"), nil)
}
