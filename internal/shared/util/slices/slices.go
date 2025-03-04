package slices

type Key[T any, K comparable] func(entity T) K

type MapFn[S any, V any] func(S) V

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
