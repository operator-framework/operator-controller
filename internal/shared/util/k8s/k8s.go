package k8s

import (
	"reflect"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CheckForUnexpectedFieldChange compares two Kubernetes objects and returns true
// if their annotations, labels, or spec have changed. This is useful for detecting
// unexpected modifications during reconciliation.
//
// The function compares:
//   - Annotations (via GetAnnotations)
//   - Labels (via GetLabels)
//   - Spec (using reflection to access the Spec field, with semantic equality)
//
// Status and finalizers are intentionally not compared, as these are expected
// to change during reconciliation.
//
// This function uses reflection to access the Spec field, so no explicit GetSpec()
// method is required. The objects must have a field named "Spec".
func CheckForUnexpectedFieldChange(a, b metav1.Object) bool {
	if !equality.Semantic.DeepEqual(a.GetAnnotations(), b.GetAnnotations()) {
		return true
	}
	if !equality.Semantic.DeepEqual(a.GetLabels(), b.GetLabels()) {
		return true
	}

	// Use reflection to access the Spec field
	aVal := reflect.ValueOf(a)
	bVal := reflect.ValueOf(b)

	// Handle pointer types
	if aVal.Kind() == reflect.Ptr {
		aVal = aVal.Elem()
	}
	if bVal.Kind() == reflect.Ptr {
		bVal = bVal.Elem()
	}

	// Get the Spec field from both objects
	aSpec := aVal.FieldByName("Spec")
	bSpec := bVal.FieldByName("Spec")

	// If either Spec field is invalid, return false (no change detected)
	if !aSpec.IsValid() || !bSpec.IsValid() {
		return false
	}

	return !equality.Semantic.DeepEqual(aSpec.Interface(), bSpec.Interface())
}
