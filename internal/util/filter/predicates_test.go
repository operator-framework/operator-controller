package filter_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/util/filter"
)

func TestAnd(t *testing.T) {
	tests := []struct {
		name       string
		predicates []filter.Predicate[int]
		input      int
		want       bool
	}{
		{
			name: "all true",
			predicates: []filter.Predicate[int]{
				func(i int) bool { return i > 0 },
				func(i int) bool { return i < 10 },
			},
			input: 5,
			want:  true,
		},
		{
			name: "one false",
			predicates: []filter.Predicate[int]{
				func(i int) bool { return i > 0 },
				func(i int) bool { return i < 5 },
			},
			input: 5,
			want:  false,
		},
		{
			name: "all false",
			predicates: []filter.Predicate[int]{
				func(i int) bool { return i > 10 },
				func(i int) bool { return i < 0 },
			},
			input: 5,
			want:  false,
		},
		{
			name:       "no predicates",
			predicates: []filter.Predicate[int]{},
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
			got := filter.And(tt.predicates...)(tt.input)
			require.Equal(t, tt.want, got, "And() = %v, want %v", got, tt.want)
		})
	}
}

func TestOr(t *testing.T) {
	tests := []struct {
		name       string
		predicates []filter.Predicate[int]
		input      int
		want       bool
	}{
		{
			name: "all true",
			predicates: []filter.Predicate[int]{
				func(i int) bool { return i > 0 },
				func(i int) bool { return i < 10 },
			},
			input: 5,
			want:  true,
		},
		{
			name: "one false",
			predicates: []filter.Predicate[int]{
				func(i int) bool { return i > 0 },
				func(i int) bool { return i < 5 },
			},
			input: 5,
			want:  true,
		},
		{
			name: "all false",
			predicates: []filter.Predicate[int]{
				func(i int) bool { return i > 10 },
				func(i int) bool { return i < 0 },
			},
			input: 5,
			want:  false,
		},
		{
			name:       "no predicates",
			predicates: []filter.Predicate[int]{},
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
			got := filter.Or(tt.predicates...)(tt.input)
			require.Equal(t, tt.want, got, "Or() = %v, want %v", got, tt.want)
		})
	}
}

func TestNot(t *testing.T) {
	tests := []struct {
		name      string
		predicate filter.Predicate[int]
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
			got := filter.Not(tt.predicate)(tt.input)
			require.Equal(t, tt.want, got, "Not() = %v, want %v", got, tt.want)
		})
	}
}
