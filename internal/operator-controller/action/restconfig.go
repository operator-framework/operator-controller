package action

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
)

const syntheticServiceAccountName = "olm.synthetic-user"

// SyntheticUserRestConfigMapper returns an AuthConfigMapper that that impersonates synthetic users and groups for Object o.
// o is expected to be a ClusterExtension. If the service account defined in o is different from 'olm.synthetic-user', the
// defaultAuthMapper will be used
func SyntheticUserRestConfigMapper(defaultAuthMapper func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error)) func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
	return func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
		cExt, err := validate(o, c)
		if err != nil {
			return nil, err
		}
		if cExt.Spec.ServiceAccount.Name != syntheticServiceAccountName {
			return defaultAuthMapper(ctx, cExt, c)
		}
		cc := rest.CopyConfig(c)
		cc.Wrap(func(rt http.RoundTripper) http.RoundTripper {
			return transport.NewImpersonatingRoundTripper(authentication.SyntheticImpersonationConfig(*cExt), rt)
		})
		return cc, nil
	}
}

// ServiceAccountRestConfigMapper returns an AuthConfigMapper scoped to the service account defined in o, which is expected to
// be a ClusterExtension
func ServiceAccountRestConfigMapper(tokenGetter *authentication.TokenGetter) func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
	return func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
		cExt, err := validate(o, c)
		if err != nil {
			return nil, err
		}

		// If ServiceAccount is not set, just use operator-controller's service account
		if cExt.Spec.ServiceAccount.Name == "" {
			return c, nil
		}

		saConfig := rest.AnonymousClientConfig(c)
		saConfig.Wrap(func(rt http.RoundTripper) http.RoundTripper {
			return &authentication.TokenInjectingRoundTripper{
				Tripper:     rt,
				TokenGetter: tokenGetter,
				Key: types.NamespacedName{
					Name:      cExt.Spec.ServiceAccount.Name,
					Namespace: cExt.Spec.Namespace,
				},
			}
		})
		return saConfig, nil
	}
}

func validate(o client.Object, c *rest.Config) (*ocv1.ClusterExtension, error) {
	if c == nil {
		return nil, fmt.Errorf("rest config is nil")
	}
	if o == nil {
		return nil, fmt.Errorf("object is nil")
	}
	cExt, ok := o.(*ocv1.ClusterExtension)
	if !ok {
		return nil, fmt.Errorf("object is not a ClusterExtension")
	}
	return cExt, nil
}
