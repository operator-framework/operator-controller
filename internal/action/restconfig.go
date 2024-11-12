package action

import (
	"context"
	"net/http"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/authentication"
)

func ServiceAccountRestConfigMapper(tokenGetter *authentication.TokenGetter) func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
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
