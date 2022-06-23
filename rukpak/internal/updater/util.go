package updater

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func ConditionsSemanticallyEqual(a, b metav1.Condition) bool {
	return a.Type == b.Type && a.Status == b.Status && a.Reason == b.Reason && a.Message == b.Message && a.ObservedGeneration == b.ObservedGeneration
}
