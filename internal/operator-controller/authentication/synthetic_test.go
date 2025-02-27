package authentication_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
)

func TestSyntheticUserName(t *testing.T) {
	syntheticUserName := authentication.SyntheticUserName(ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-ext",
		},
	})
	require.Equal(t, "olmv1:clusterextensions:my-ext:admin", syntheticUserName)
}

func TestSyntheticGroups(t *testing.T) {
	syntheticGroups := authentication.SyntheticGroups(ocv1.ClusterExtension{})
	require.Equal(t, []string{
		"olmv1:clusterextensions:admin",
		"system:authenticated",
	}, syntheticGroups)
}

func TestSyntheticImpersonationConfig(t *testing.T) {
	config := authentication.SyntheticImpersonationConfig(ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-ext",
		},
	})
	require.Equal(t, "olmv1:clusterextensions:my-ext:admin", config.UserName)
	require.Equal(t, []string{
		"olmv1:clusterextensions:admin",
		"system:authenticated",
	}, config.Groups)
	require.Empty(t, config.UID)
	require.Empty(t, config.Extra)
}
