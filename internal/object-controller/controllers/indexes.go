//go:build !standard

package controllers

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// clusterObjectSetGroupIndex is the field index on ClusterObjectSet spec.group. It
// is registered once per manager by SetupIndexes and used to look up all revisions
// in a group.
const clusterObjectSetGroupIndex = ".spec.group"

// SetupIndexes registers the cache indexes used to look up ClusterObjectSet
// revisions by group. Both the ClusterObjectSet and ClusterObjectDeployment
// controllers rely on the spec.group index, so it is registered once per manager
// rather than per controller.
func SetupIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	return indexer.IndexField(ctx, &ocv1.ClusterObjectSet{}, clusterObjectSetGroupIndex, func(obj client.Object) []string {
		cos, ok := obj.(*ocv1.ClusterObjectSet)
		if !ok || cos.Spec.Group == "" {
			return nil
		}
		return []string{cos.Spec.Group}
	})
}
