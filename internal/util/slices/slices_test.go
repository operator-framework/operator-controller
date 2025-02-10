package slices_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/util/filter"
	"github.com/operator-framework/operator-controller/internal/util/slices"
)

func TestFilter(t *testing.T) {
	tests := []struct {
		name      string
		slice     []int
		predicate filter.Predicate[int]
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
		predicate filter.Predicate[int]
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
