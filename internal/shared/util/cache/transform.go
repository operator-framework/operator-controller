package cache

import (
	"maps"

	toolscache "k8s.io/client-go/tools/cache"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// stripAnnotations removes memory-heavy annotations that aren't needed for controller operations.
func stripAnnotations(obj interface{}) (interface{}, error) {
	if metaObj, ok := obj.(client.Object); ok {
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

// StripManagedFieldsAndAnnotations returns a cache transform function that removes
// memory-heavy fields that aren't needed for controller operations.
// This significantly reduces memory usage in informer caches by removing:
// - Managed fields (can be several KB per object)
// - kubectl.kubernetes.io/last-applied-configuration annotation (can be very large)
//
// Use this function as a DefaultTransform in controller-runtime cache.Options
// to reduce memory overhead across all cached objects.
//
// Example:
//
//	cacheOptions := cache.Options{
//	    DefaultTransform: cacheutil.StripManagedFieldsAndAnnotations(),
//	}
func StripManagedFieldsAndAnnotations() toolscache.TransformFunc {
	// Use controller-runtime's built-in TransformStripManagedFields and compose it
	// with our custom annotation stripping transform
	managedFieldsTransform := crcache.TransformStripManagedFields()

	return func(obj interface{}) (interface{}, error) {
		// First strip managed fields using controller-runtime's transform
		obj, err := managedFieldsTransform(obj)
		if err != nil {
			return obj, err
		}

		// Then strip the large annotations
		return stripAnnotations(obj)
	}
}

// StripAnnotations returns a cache transform function that removes
// memory-heavy annotation fields that aren't needed for controller operations.
// This significantly reduces memory usage in informer caches by removing:
// - kubectl.kubernetes.io/last-applied-configuration annotation (can be very large)
//
// Use this function as a DefaultTransform in controller-runtime cache.Options
// to reduce memory overhead across all cached objects.
//
// Example:
//
//	cacheOptions := cache.Options{
//	    DefaultTransform: cacheutil.StripAnnotations(),
//	}
func StripAnnotations() toolscache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		// Strip the large annotations
		return stripAnnotations(obj)
	}
}

// ApplyStripAnnotationsTransform applies the strip transform directly to an object.
// This is a convenience function for cases where you need to strip fields
// from an object outside of the cache transform context.
//
// Note: This function never returns an error in practice, but returns error
// to satisfy the TransformFunc interface.
func ApplyStripAnnotationsTransform(obj client.Object) error {
	transform := StripAnnotations()
	_, err := transform(obj)
	return err
}
