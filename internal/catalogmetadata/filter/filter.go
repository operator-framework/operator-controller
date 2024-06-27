package filter

import (
	"slices"
)

// Predicate returns true if the object should be kept when filtering
type Predicate[T any] func(entity T) bool

// Filter filters a slice accordingly to
func Filter[T any](in []T, test Predicate[T]) []T {
	return slices.DeleteFunc(in, Not(test))
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
