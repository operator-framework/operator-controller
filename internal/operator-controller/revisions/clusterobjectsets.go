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

// ListForGroup returns all COS revisions in the group, oldest first.
// Owner references do not affect revision association. Lifecycle filtering is
// left to callers.
func ListForGroup(ctx context.Context, reader client.Reader, group string) ([]ocv1.ClusterObjectSet, error) {
	list := &ocv1.ClusterObjectSetList{}
	if err := reader.List(ctx, list, client.MatchingFields{".spec.group": group}); err != nil {
		return nil, err
	}
	slices.SortFunc(list.Items, func(a, b ocv1.ClusterObjectSet) int {
		return cmp.Compare(a.Spec.Revision, b.Spec.Revision)
	})
	return list.Items, nil
}
