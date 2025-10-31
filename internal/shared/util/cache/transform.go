package cache

import (
	"maps"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StripManagedFieldsAndAnnotations is a cache transform function that removes
// memory-heavy fields that aren't needed for controller operations.
// This significantly reduces memory usage in informer caches by removing:
// - Managed fields (can be several KB per object)
// - kubectl.kubernetes.io/last-applied-configuration annotation (can be very large)
//
// Use this function as a DefaultTransform in controller-runtime cache.Options
// to reduce memory overhead across all cached objects.
//
// Note: This function signature returns an error to satisfy the cache.TransformFunc
// interface, but it never returns an error in practice.
func StripManagedFieldsAndAnnotations(obj interface{}) (interface{}, error) {
	if metaObj, ok := obj.(client.Object); ok {
		// Remove managed fields - these can be several KB per object
		metaObj.SetManagedFields(nil)

		// Remove the last-applied-configuration annotation which can be very large
		// Clone the annotations map to avoid modifying shared references
		annotations := metaObj.GetAnnotations()
		if annotations != nil {
			annotations = maps.Clone(annotations)
			delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
			if len(annotations) == 0 {
				metaObj.SetAnnotations(nil)
			} else {
				metaObj.SetAnnotations(annotations)
			}
		}
	}
	return obj, nil
}
