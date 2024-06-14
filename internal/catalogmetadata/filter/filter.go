package filter

import (
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

// Predicate returns true if the object should be kept when filtering
type Predicate[T catalogmetadata.Schemas] func(entity *T) (bool, []string)

// Filter filters a slice accordingly to
func Filter[T catalogmetadata.Schemas](in []*T, test Predicate[T]) ([]*T, []string) {
	out := []*T{}
	var errs []string
	for i := range in {
		res, e := test(in[i])
		if res {
			out = append(out, in[i])
		} else {
			errs = append(errs, e...)
		}
	}
	return out, errs
}

func And[T catalogmetadata.Schemas](predicates ...Predicate[T]) Predicate[T] {
	return func(obj *T) (bool, []string) {
		for _, predicate := range predicates {
			if res, errs := predicate(obj); !res {
				return false, errs
			}
		}
		return true, nil
	}
}

func Or[T catalogmetadata.Schemas](predicates ...Predicate[T]) Predicate[T] {
	return func(obj *T) (bool, []string) {
		var errs []string
		for _, predicate := range predicates {
			if res, e := predicate(obj); !res {
				errs = append(errs, e...)
			} else {
				return true, nil
			}
		}
		return false, errs
	}
}

func Not[T catalogmetadata.Schemas](predicate Predicate[T]) Predicate[T] {
	return func(obj *T) (bool, []string) {
		predicateTrue, errs := predicate(obj)
		if predicateTrue {
			return false, errs
		}
		return true, nil
	}
}
