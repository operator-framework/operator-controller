package filter

import "slices"

// Predicate returns true if the object should be kept when filtering
type Predicate[T any] func(entity T) bool

type Key[T any, K comparable] func(entity T) K

type MapFn[S any, V any] func(S) V

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

// Filter creates a new slice with all elements from s for which the test returns true
func Filter[T any](s []T, test Predicate[T]) []T {
	var out []T
	for i := 0; i < len(s); i++ {
		if test(s[i]) {
			out = append(out, s[i])
		}
	}
	return slices.Clip(out)
}

// InPlace modifies s by removing any element for which test returns false.
// InPlace zeroes the elements between the new length and the original length in s.
// The returned slice is of the new length.
func InPlace[T any](s []T, test Predicate[T]) []T {
	return slices.DeleteFunc(s, Not(test))
}

func GroupBy[T any, K comparable](s []T, key Key[T, K]) map[K][]T {
	out := map[K][]T{}
	for _, value := range s {
		k := key(value)
		out[k] = append(out[k], value)
	}
	return out
}

func Map[S, V any](s []S, mapper MapFn[S, V]) []V {
	out := make([]V, len(s))
	for i := 0; i < len(s); i++ {
		out[i] = mapper(s[i])
	}
	return out
}
