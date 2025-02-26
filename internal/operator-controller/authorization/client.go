package authorization

import (
	"context"

	authorizationv1client "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type NewForConfigFunc func(*rest.Config) (authorizationv1client.AuthorizationV1Interface, error)

// AuthorizationClient is an interface exposing only the needed functionality
type AuthorizationClient interface {
	CheckContentPermissions(ctx context.Context, objects []client.Object, ext *ocv1.ClusterExtension) error
}

// clientImpl wraps the underlying authorization client
type clientImpl struct {
	authClient authorizationv1client.AuthorizationV1Interface
}

// NewClient wraps an authorizationv1client.AuthorizationV1Interface
func NewClient(authClient authorizationv1client.AuthorizationV1Interface) AuthorizationClient {
	return &clientImpl{authClient: authClient}
}

// CheckContentPermissions is the public method that internally calls the generic check
func (c *clientImpl) CheckContentPermissions(ctx context.Context, objects []client.Object, ext *ocv1.ClusterExtension) error {
	return CheckObjectPermissions(ctx, c.authClient, objects, ext)
}
