//go:build !standard

package controllers_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
)

const clusterObjectDeploymentName = "test-cod-1"

func Test_ClusterObjectDeploymentReconciler_Reconcile(t *testing.T) {
	testScheme := newScheme(t)

	for _, tc := range []struct {
		name         string
		existingObjs []client.Object
	}{
		{
			name:         "returns no error when the ClusterObjectDeployment does not exist",
			existingObjs: nil,
		},
		{
			name: "returns no error for an existing ClusterObjectDeployment",
			existingObjs: []client.Object{
				&ocv1.ClusterObjectDeployment{
					ObjectMeta: metav1.ObjectMeta{Name: clusterObjectDeploymentName},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testClient := fake.NewClientBuilder().
				WithScheme(testScheme).
				WithStatusSubresource(&ocv1.ClusterObjectDeployment{}).
				WithObjects(tc.existingObjs...).
				Build()

			result, err := (&controllers.ClusterObjectDeploymentReconciler{
				Client: testClient,
			}).Reconcile(t.Context(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: clusterObjectDeploymentName,
				},
			})

			require.NoError(t, err)
			require.Equal(t, ctrl.Result{}, result)
		})
	}
}
