package action

import (
	"context"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterAdminRestConfigMapper returns a config with cluster-admin permissions
func ClusterAdminRestConfigMapper(adminConfig *rest.Config) func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config,
	error) {
	return func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
		return rest.CopyConfig(adminConfig), nil
	}
}
