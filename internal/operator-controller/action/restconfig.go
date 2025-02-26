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
)

func ClusterExtensionUserRestConfigMapper(tokenGetter *authentication.TokenGetter) func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
	saRestConfigMapper := serviceAccountRestConfigMapper(tokenGetter)
	synthRestConfigMapper := sythenticUserRestConfigMapper()

	return func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
		cExt := o.(*ocv1.ClusterExtension)
		if cExt.Spec.ServiceAccount != nil { //nolint:staticcheck
			return saRestConfigMapper(ctx, o, c)
		}
		return synthRestConfigMapper(ctx, o, c)
	}
}

func sythenticUserRestConfigMapper() func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
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
			Name:      cExt.Spec.ServiceAccount.Name, //nolint:staticcheck
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
