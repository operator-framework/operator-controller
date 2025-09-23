package action

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
)

// ServiceAccountRestConfigMapper returns an AuthConfigMapper scoped to the service account defined in o, which is expected to
// be a ClusterExtension
func ServiceAccountRestConfigMapper(tokenGetter *authentication.TokenGetter) func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
	return func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
		cExt, err := validate(o, c)
		if err != nil {
			return nil, err
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

// ClusterAdminRestConfigMapper returns a config with cluster-admin permissions
func ClusterAdminRestConfigMapper(adminConfig *rest.Config) func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config,
	error) {
	return func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
		return rest.CopyConfig(adminConfig), nil
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
