package slices

import (
	"slices"

	"github.com/operator-framework/operator-controller/internal/util/filter"
)

// Filter creates a new slice with all elements from s for which the test returns true
func Filter[T any](s []T, test filter.Predicate[T]) []T {
	out := make([]T, 0, len(s))
	for i := 0; i < len(s); i++ {
		if test(s[i]) {
			out = append(out, s[i])
		}
	}
	return slices.Clip(out)
}

// RemoveInPlace removes all elements from s for which test returns true.
// Elements between new length and original length are zeroed out.
func RemoveInPlace[T any](s []T, test filter.Predicate[T]) []T {
	return slices.DeleteFunc(s, filter.Not(test))
}
