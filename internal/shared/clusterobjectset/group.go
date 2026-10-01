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

package clusterobjectset

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// GroupField is the cache index key used to find revisions in the same group.
const GroupField = ".spec.group"

// ExtractGroup returns the group indexed for a ClusterObjectSet.
func ExtractGroup(obj client.Object) []string {
	cos, ok := obj.(*ocv1.ClusterObjectSet)
	if !ok || cos.Spec.Group == "" {
		return nil
	}
	return []string{cos.Spec.Group}
}
