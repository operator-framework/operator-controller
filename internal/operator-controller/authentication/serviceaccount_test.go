package authentication_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
)

func TestServiceAccountImpersonationConfig(t *testing.T) {
	config := authentication.ServiceAccountImpersonationConfig(ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-ext",
		},
		Spec: ocv1.ClusterExtensionSpec{Namespace: "my-namespace", ServiceAccount: ocv1.ServiceAccountReference{Name: "my-service-account"}},
	})
	require.Equal(t, "system:serviceaccount:my-namespace:my-service-account", config.UserName)
	require.Nil(t, config.Groups)
	require.Empty(t, config.UID)
	require.Empty(t, config.Extra)
}
