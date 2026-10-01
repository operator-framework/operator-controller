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

	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// ListForClusterExtension returns all COS revisions in the extension's group, oldest first.
// CE-generated revisions use the extension name as their group. Owner references
// do not affect revision association. Lifecycle filtering is left to callers.
func ListForClusterExtension(ctx context.Context, reader client.Reader, ext *ocv1.ClusterExtension) ([]ocv1.ClusterObjectSet, error) {
	list := &ocv1.ClusterObjectSetList{}
	if err := reader.List(ctx, list, client.MatchingFields{".spec.group": ext.Name}); err != nil {
		return nil, err
	}
	slices.SortFunc(list.Items, func(a, b ocv1.ClusterObjectSet) int {
		return cmp.Compare(a.Spec.Revision, b.Spec.Revision)
	})
	return list.Items, nil
}
