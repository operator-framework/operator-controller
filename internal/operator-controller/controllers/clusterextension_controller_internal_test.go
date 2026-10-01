package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestClusterExtensionRequestsForClusterObjectSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		obj  client.Object
		want []reconcile.Request
	}{
		{
			name: "group without owner",
			obj:  &ocv1.ClusterObjectSet{Spec: ocv1.ClusterObjectSetSpec{Group: "my-extension"}},
			want: []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "my-extension"}}},
		},
		{
			name: "group takes precedence over owner metadata",
			obj: &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Labels:          map[string]string{"olm.operatorframework.io/owner-name": "other"},
					OwnerReferences: []metav1.OwnerReference{{Kind: ocv1.ClusterExtensionKind, Name: "other", Controller: ptr.To(true)}},
				},
				Spec: ocv1.ClusterObjectSetSpec{Group: "my-extension"},
			},
			want: []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "my-extension"}}},
		},
		{name: "empty group", obj: &ocv1.ClusterObjectSet{}},
		{name: "other resource", obj: &corev1.ConfigMap{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, clusterExtensionRequestsForClusterObjectSet(t.Context(), tc.obj))
		})
	}
}
