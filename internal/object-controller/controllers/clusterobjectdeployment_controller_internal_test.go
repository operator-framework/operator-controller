//go:build !standard

package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func Test_mapClusterObjectSetToClusterObjectDeployment(t *testing.T) {
	for _, tc := range []struct {
		name string
		obj  client.Object
		want []ctrl.Request
	}{
		{
			name: "maps spec.group to a ClusterObjectDeployment request",
			obj: &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{Name: "cod-1-1"},
				Spec:       ocv1.ClusterObjectSetSpec{Group: "cod-1"},
			},
			want: []ctrl.Request{{NamespacedName: types.NamespacedName{Name: "cod-1"}}},
		},
		{
			name: "returns nil when spec.group is empty",
			obj: &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{Name: "cod-1-1"},
			},
			want: nil,
		},
		{
			name: "returns nil for a non-ClusterObjectSet object",
			obj:  &ocv1.ClusterObjectDeployment{ObjectMeta: metav1.ObjectMeta{Name: "cod-1"}},
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mapClusterObjectSetToClusterObjectDeployment(t.Context(), tc.obj)
			require.Equal(t, tc.want, got)
		})
	}
}
