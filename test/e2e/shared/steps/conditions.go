package steps

import (
	"context"
	"encoding/json"
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// resourceConditions reads metadata and conditions from the same API response so
// status assertions cannot accept a condition from an earlier spec generation.
type resourceConditions struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Status   struct {
		Conditions []metav1.Condition `json:"conditions"`
	} `json:"status"`
}

func readResourceConditions(ctx context.Context, resourceType, resourceName string) (*resourceConditions, error) {
	out, err := k8sClient(ctx, "get", resourceType, resourceName, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("reading %s %q: %w", resourceType, resourceName, err)
	}
	var resource resourceConditions
	if err := json.Unmarshal([]byte(out), &resource); err != nil {
		return nil, fmt.Errorf("unmarshaling %s %q: %w", resourceType, resourceName, err)
	}
	return &resource, nil
}

func generationObserved(generation, observedGeneration int64) bool {
	return generation > 0 && observedGeneration == generation
}

// currentCondition returns only the requested condition, and only if it reflects
// the current generation. An unrelated up-to-date condition is not sufficient.
func currentCondition(generation int64, conditions []metav1.Condition, conditionType string) *metav1.Condition {
	condition := apimeta.FindStatusCondition(conditions, conditionType)
	if condition == nil || !generationObserved(generation, condition.ObservedGeneration) {
		return nil
	}
	return condition
}

func conditionMatches(condition *metav1.Condition, conditionStatus metav1.ConditionStatus, reason *string, msgCmp msgMatchFn) bool {
	return condition != nil && condition.Status == conditionStatus &&
		(reason == nil || condition.Reason == *reason) &&
		(msgCmp == nil || msgCmp(condition.Message))
}
