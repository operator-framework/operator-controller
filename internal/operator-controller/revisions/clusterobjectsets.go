/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package revisions

import (
	"cmp"
	"context"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// ListForClusterExtension returns the extension's COS revisions, oldest first.
// CE-generated revisions use the extension name as their group. Controller owner
// kind and name distinguish them from unrelated revisions sharing that group,
// using the same owner identity as the COS controller's revision sequences.
func ListForClusterExtension(ctx context.Context, reader client.Reader, ext *ocv1.ClusterExtension) ([]ocv1.ClusterObjectSet, error) {
	list := &ocv1.ClusterObjectSetList{}
	if err := reader.List(ctx, list, client.MatchingFields{".spec.group": ext.Name}); err != nil {
		return nil, err
	}
	list.Items = slices.DeleteFunc(list.Items, func(cos ocv1.ClusterObjectSet) bool {
		owner := metav1.GetControllerOf(&cos)
		return owner == nil || owner.Kind != ocv1.ClusterExtensionKind || owner.Name != ext.Name
	})
	slices.SortFunc(list.Items, func(a, b ocv1.ClusterObjectSet) int {
		return cmp.Compare(a.Spec.Revision, b.Spec.Revision)
	})
	return list.Items, nil
}
