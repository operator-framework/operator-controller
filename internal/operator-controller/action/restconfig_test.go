package action

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

const (
	saAccountWrapper = "service account wrapper"
	synthUserWrapper = "synthetic user wrapper"
)

func fakeRestConfigWrapper() clusterExtensionRestConfigMapper {
	// The rest config's host field is artificially used to differentiate between the wrappers
	return clusterExtensionRestConfigMapper{
		saRestConfigMapper: func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
			return &rest.Config{
				Host: saAccountWrapper,
			}, nil
		},
		synthUserRestConfigMapper: func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
			return &rest.Config{
				Host: synthUserWrapper,
			}, nil
		},
	}
}

func TestMapper_SyntheticPermissionsEnabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.SyntheticPermissions, true)

	for _, tc := range []struct {
		description        string
		serviceAccountName string
		expectedMapper     string
		fgEnabled          bool
	}{
		{
			description:        "user service account wrapper if extension service account is _not_ called olm.synthetic-user",
			serviceAccountName: "not.olm.synthetic-user",
			expectedMapper:     saAccountWrapper,
			fgEnabled:          true,
		}, {
			description:        "user synthetic user wrapper is extension service account is called olm.synthetic-user",
			serviceAccountName: "olm.synthetic-user",
			expectedMapper:     synthUserWrapper,
			fgEnabled:          true,
		},
	} {
		t.Run(tc.description, func(t *testing.T) {
			m := fakeRestConfigWrapper()
			mapper := m.mapper()
			ext := &ocv1.ClusterExtension{
				Spec: ocv1.ClusterExtensionSpec{
					ServiceAccount: ocv1.ServiceAccountReference{
						Name: tc.serviceAccountName,
					},
				},
			}
			cfg, err := mapper(context.Background(), ext, &rest.Config{})
			require.NoError(t, err)

			// The rest config's host field is artificially used to differentiate between the wrappers
			require.Equal(t, tc.expectedMapper, cfg.Host)
		})
	}
}

func TestMapper_SyntheticPermissionsDisabled(t *testing.T) {
	m := fakeRestConfigWrapper()
	mapper := m.mapper()
	ext := &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: "olm.synthetic-user",
			},
		},
	}
	cfg, err := mapper(context.Background(), ext, &rest.Config{})
	require.NoError(t, err)

	// The rest config's host field is artificially used to differentiate between the wrappers
	require.Equal(t, saAccountWrapper, cfg.Host)
}
