//go:build !standard

package controllers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// clusterObjectSetGroupIndex is the field index on ClusterObjectSet spec.group. It
// is registered once per manager by SetupIndexes and used to look up all revisions
// in a group.
const clusterObjectSetGroupIndex = ".spec.group"

// clusterObjectSetSourceRefIndex maps a source Secret's namespace/name to all
// revisions that reference it, regardless of the Secret's owner references.
const clusterObjectSetSourceRefIndex = ".spec.phases.objects.ref"

// SetupIndexes registers the cache indexes used to look up ClusterObjectSet
// revisions by group and referenced Secret. The group index is shared with the
// ClusterObjectDeployment controller, so indexes are registered once per manager.
func SetupIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	if err := indexer.IndexField(ctx, &ocv1.ClusterObjectSet{}, clusterObjectSetGroupIndex, func(obj client.Object) []string {
		cos, ok := obj.(*ocv1.ClusterObjectSet)
		if !ok || cos.Spec.Group == "" {
			return nil
		}
		return []string{cos.Spec.Group}
	}); err != nil {
		return fmt.Errorf("indexing ClusterObjectSet groups: %w", err)
	}
	return indexer.IndexField(ctx, &ocv1.ClusterObjectSet{}, clusterObjectSetSourceRefIndex, func(obj client.Object) []string {
		cos, ok := obj.(*ocv1.ClusterObjectSet)
		if !ok {
			return nil
		}
		refs := sets.New[string]()
		for _, phase := range cos.Spec.Phases {
			for _, obj := range phase.Objects {
				if obj.Ref.Name == "" {
					continue
				}
				key := client.ObjectKey{Namespace: obj.Ref.Namespace, Name: obj.Ref.Name}
				refs.Insert(key.String())
			}
		}
		return sets.List(refs)
	})
}
