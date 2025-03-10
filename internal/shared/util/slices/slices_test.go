package slices_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	slicesutil "github.com/operator-framework/operator-controller/internal/shared/util/slices"
)

func Test_Map(t *testing.T) {
	in := []int{1, 2, 3, 4, 5}
	doubleIt := func(val int) int {
		return 2 * val
	}
	require.Equal(t, []int{2, 4, 6, 8, 10}, slicesutil.Map(in, doubleIt))
}

func Test_GroupBy(t *testing.T) {
	in := []int{1, 2, 3, 4, 5}
	oddOrEven := func(val int) string {
		if val%2 == 0 {
			return "even"
		}
		return "odd"
	}
	require.Equal(t, map[string][]int{
		"even": {2, 4},
		"odd":  {1, 3, 5},
	}, slicesutil.GroupBy(in, oddOrEven))
}
