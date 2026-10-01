//go:build !standard

package controllers_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/test"
)

func TestSetupIndexes_GroupLookup(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))
	cl := test.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), controllers.SetupIndexes).
		WithObjects(
			&ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "matching-group",
					Labels: map[string]string{
						"olm.operatorframework.io/owner-name": "other-group",
					},
				},
				Spec: ocv1.ClusterObjectSetSpec{Group: "my-group"},
			},
			&ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-group",
					Labels: map[string]string{
						"olm.operatorframework.io/owner-name": "my-group",
					},
				},
				Spec: ocv1.ClusterObjectSetSpec{Group: "other-group"},
			},
			&ocv1.ClusterObjectSet{ObjectMeta: metav1.ObjectMeta{Name: "missing-group"}},
		).
		Build()

	for _, tc := range []struct {
		group string
		want  []string
	}{
		{group: "my-group", want: []string{"matching-group"}},
		{group: "other-group", want: []string{"other-group"}},
		{group: ""},
	} {
		t.Run(tc.group, func(t *testing.T) {
			list := &ocv1.ClusterObjectSetList{}
			require.NoError(t, cl.List(t.Context(), list, client.MatchingFields{".spec.group": tc.group}))
			names := make([]string, 0, len(list.Items))
			for _, cos := range list.Items {
				names = append(names, cos.Name)
			}
			require.ElementsMatch(t, tc.want, names)
		})
	}
}
