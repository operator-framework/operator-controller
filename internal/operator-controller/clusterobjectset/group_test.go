package clusterobjectset

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestExtractGroup(t *testing.T) {
	for _, tc := range []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "group is independent of owner metadata",
			obj: &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"olm.operatorframework.io/owner-name": "other"}},
				Spec:       ocv1.ClusterObjectSetSpec{Group: "my-group"},
			},
			want: []string{"my-group"},
		},
		{name: "missing group", obj: &ocv1.ClusterObjectSet{}},
		{name: "other resource", obj: &corev1.ConfigMap{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ExtractGroup(tc.obj))
		})
	}
}
