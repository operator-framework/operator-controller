package slices_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/util/slices"
)

func TestAnd(t *testing.T) {
	tests := []struct {
		name       string
		predicates []slices.Predicate[int]
		input      int
		want       bool
	}{
		{
			name: "all true",
			predicates: []slices.Predicate[int]{
				func(i int) bool { return i > 0 },
				func(i int) bool { return i < 10 },
			},
			input: 5,
			want:  true,
		},
		{
			name: "one false",
			predicates: []slices.Predicate[int]{
				func(i int) bool { return i > 0 },
				func(i int) bool { return i < 5 },
			},
			input: 5,
			want:  false,
		},
		{
			name: "all false",
			predicates: []slices.Predicate[int]{
				func(i int) bool { return i > 10 },
				func(i int) bool { return i < 0 },
			},
			input: 5,
			want:  false,
		},
		{
			name:       "no predicates",
			predicates: []slices.Predicate[int]{},
			input:      5,
			want:       true,
		},
		{
			name:       "nil predicates",
			predicates: nil,
			input:      5,
			want:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slices.And(tt.predicates...)(tt.input)
			require.Equal(t, tt.want, got, "And() = %v, want %v", got, tt.want)
		})
	}
}

func TestOr(t *testing.T) {
	tests := []struct {
		name       string
		predicates []slices.Predicate[int]
		input      int
		want       bool
	}{
		{
			name: "all true",
			predicates: []slices.Predicate[int]{
				func(i int) bool { return i > 0 },
				func(i int) bool { return i < 10 },
			},
			input: 5,
			want:  true,
		},
		{
			name: "one false",
			predicates: []slices.Predicate[int]{
				func(i int) bool { return i > 0 },
				func(i int) bool { return i < 5 },
			},
			input: 5,
			want:  true,
		},
		{
			name: "all false",
			predicates: []slices.Predicate[int]{
				func(i int) bool { return i > 10 },
				func(i int) bool { return i < 0 },
			},
			input: 5,
			want:  false,
		},
		{
			name:       "no predicates",
			predicates: []slices.Predicate[int]{},
			input:      5,
			want:       false,
		},
		{
			name:       "nil predicates",
			predicates: nil,
			input:      5,
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slices.Or(tt.predicates...)(tt.input)
			require.Equal(t, tt.want, got, "Or() = %v, want %v", got, tt.want)
		})
	}
}

func TestNot(t *testing.T) {
	tests := []struct {
		name      string
		predicate slices.Predicate[int]
		input     int
		want      bool
	}{
		{
			name:      "predicate is true",
			predicate: func(i int) bool { return i > 0 },
			input:     5,
			want:      false,
		},
		{
			name:      "predicate is false",
			predicate: func(i int) bool { return i > 3 },
			input:     2,
			want:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slices.Not(tt.predicate)(tt.input)
			require.Equal(t, tt.want, got, "Not() = %v, want %v", got, tt.want)
		})
	}
}

func TestFilter(t *testing.T) {
	tests := []struct {
		name      string
		slice     []int
		predicate slices.Predicate[int]
		want      []int
	}{
		{
			name:      "all match",
			slice:     []int{1, 2, 3, 4, 5},
			predicate: func(i int) bool { return i > 0 },
			want:      []int{1, 2, 3, 4, 5},
		},
		{
			name:      "some match",
			slice:     []int{1, 2, 3, 4, 5},
			predicate: func(i int) bool { return i > 3 },
			want:      []int{4, 5},
		},
		{
			name:      "none match",
			slice:     []int{1, 2, 3, 4, 5},
			predicate: func(i int) bool { return i > 5 },
			want:      []int{},
		},
		{
			name:      "empty slice",
			slice:     []int{},
			predicate: func(i int) bool { return i > 5 },
			want:      []int{},
		},
		{
			name:      "nil slice",
			slice:     nil,
			predicate: func(i int) bool { return i > 5 },
			want:      []int{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slices.Filter(tt.slice, tt.predicate)
			require.Equal(t, tt.want, got, "Filter() = %v, want %v", got, tt.want)
		})
	}
}

func TestRemoveInPlace(t *testing.T) {
	tests := []struct {
		name      string
		slice     []int
		predicate slices.Predicate[int]
		want      []int
	}{
		{
			name:      "all match",
			slice:     []int{1, 2, 3, 4, 5},
			predicate: func(i int) bool { return i > 0 },
			want:      []int{1, 2, 3, 4, 5},
		},
		{
			name:      "some match",
			slice:     []int{1, 2, 3, 4, 5},
			predicate: func(i int) bool { return i > 3 },
			want:      []int{4, 5, 0, 0, 0},
		},
		{
			name:      "none match",
			slice:     []int{1, 2, 3, 4, 5},
			predicate: func(i int) bool { return i > 5 },
			want:      []int{0, 0, 0, 0, 0},
		},
		{
			name:      "empty slice",
			slice:     []int{},
			predicate: func(i int) bool { return i > 5 },
			want:      []int{},
		},
		{
			name:      "nil slice",
			slice:     nil,
			predicate: func(i int) bool { return i > 5 },
			want:      nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = slices.RemoveInPlace(tt.slice, tt.predicate)
			require.Equal(t, tt.want, tt.slice, "Filter() = %v, want %v", tt.slice, tt.want)
		})
	}
}
