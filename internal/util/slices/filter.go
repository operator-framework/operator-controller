package slices

import (
	"slices"
)

// Predicate returns true if the object should be kept when filtering
type Predicate[T any] func(entity T) bool

// Filter creates a new slice with all elements from s for which the test returns true
func Filter[T any](s []T, test Predicate[T]) []T {
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
func RemoveInPlace[T any](s []T, test Predicate[T]) []T {
	return slices.DeleteFunc(s, Not(test))
}

func And[T any](predicates ...Predicate[T]) Predicate[T] {
	return func(obj T) bool {
		for _, predicate := range predicates {
			if !predicate(obj) {
				return false
			}
		}
		return true
	}
}

func Or[T any](predicates ...Predicate[T]) Predicate[T] {
	return func(obj T) bool {
		for _, predicate := range predicates {
			if predicate(obj) {
				return true
			}
		}
		return false
	}
}

func Not[T any](predicate Predicate[T]) Predicate[T] {
	return func(obj T) bool {
		return !predicate(obj)
	}
}
