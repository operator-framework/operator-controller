package authentication_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
)

func TestSyntheticImpersonationConfig(t *testing.T) {
	config := authentication.SyntheticImpersonationConfig(ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-ext",
		},
	})
	require.Equal(t, "olm:clusterextension:my-ext", config.UserName)
	require.Equal(t, []string{
		"olm:clusterextensions",
	}, config.Groups)
	require.Empty(t, config.UID)
	require.Empty(t, config.Extra)
}
