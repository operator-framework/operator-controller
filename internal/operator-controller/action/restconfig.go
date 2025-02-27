package action

import (
	"context"
	"net/http"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

const syntheticServiceAccountName = "olm.synthetic-user"

type clusterExtensionRestConfigMapper struct {
	saRestConfigMapper        func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error)
	synthUserRestConfigMapper func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error)
}

func (m *clusterExtensionRestConfigMapper) mapper() func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
	synthAuthFeatureEnabled := features.OperatorControllerFeatureGate.Enabled(features.SyntheticPermissions)
	return func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
		cExt := o.(*ocv1.ClusterExtension)
		if synthAuthFeatureEnabled && cExt.Spec.ServiceAccount.Name == syntheticServiceAccountName {
			return m.synthUserRestConfigMapper(ctx, o, c)
		}
		return m.saRestConfigMapper(ctx, o, c)
	}
}

func ClusterExtensionUserRestConfigMapper(tokenGetter *authentication.TokenGetter) func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
	m := &clusterExtensionRestConfigMapper{
		saRestConfigMapper:        serviceAccountRestConfigMapper(tokenGetter),
		synthUserRestConfigMapper: syntheticUserRestConfigMapper(),
	}
	return m.mapper()
}

func syntheticUserRestConfigMapper() func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
	return func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
		cExt := o.(*ocv1.ClusterExtension)
		cc := rest.CopyConfig(c)
		cc.Wrap(func(rt http.RoundTripper) http.RoundTripper {
			return transport.NewImpersonatingRoundTripper(authentication.SyntheticImpersonationConfig(*cExt), rt)
		})
		return cc, nil
	}
}

func serviceAccountRestConfigMapper(tokenGetter *authentication.TokenGetter) func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
	return func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
		cExt := o.(*ocv1.ClusterExtension)
		saKey := types.NamespacedName{
			Name:      cExt.Spec.ServiceAccount.Name,
			Namespace: cExt.Spec.Namespace,
		}
		saConfig := rest.AnonymousClientConfig(c)
		saConfig.Wrap(func(rt http.RoundTripper) http.RoundTripper {
			return &authentication.TokenInjectingRoundTripper{
				Tripper:     rt,
				TokenGetter: tokenGetter,
				Key:         saKey,
			}
		})
		return saConfig, nil
	}
}
