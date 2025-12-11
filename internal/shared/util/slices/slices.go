package slices

func Map[I any, O any](in []I, f func(I) O) []O {
	out := make([]O, len(in))
	for i := range in {
		out[i] = f(in[i])
	}
	return out
}
