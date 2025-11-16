/*
Copyright 2025.

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

package finalizer

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const (
	FinalizerPrefix = "olm.operatorframework.io/"
)

// UpdateFinalizers sets the finalizers on an object to exactly the provided list using server-side apply.
// If no finalizers are supplied, all finalizers will be removed from the object.
// If one finalizer is supplied, all other finalizers will be removed and only the supplied one will remain.
// Returns (true, nil) if the finalizers were changed, (false, nil) if they were already set to the desired value.
// Note: This function will update the passed object with the server response.
func UpdateFinalizers(ctx context.Context, owner string, c client.Client, obj client.Object, finalizers ...string) (bool, error) {
	// Sort the desired finalizers for consistent ordering
	newFinalizers := slices.Clone(finalizers)
	if newFinalizers == nil {
		newFinalizers = []string{}
	}
	// Possibly overkill, but it will ensure our finalizers use the proper prefix
	for _, s := range newFinalizers {
		if !strings.HasPrefix(s, FinalizerPrefix) {
			panic(fmt.Sprintf("finalizer does not have %q prefix: %q", FinalizerPrefix, s))
		}
	}
	sort.Strings(newFinalizers)

	// Check if the current finalizers already match the desired state
	// Remove any non-"olm.operatorframework.io" finalizers (ones we don't manage) from the list
	currentFinalizers := obj.GetFinalizers()
	currentSorted := slices.Clone(currentFinalizers)
	currentSorted = slices.DeleteFunc(currentSorted, func(f string) bool {
		return !strings.HasPrefix(f, FinalizerPrefix)
	})
	if currentSorted == nil {
		currentSorted = []string{}
	}
	sort.Strings(currentSorted)

	// With only "olm.operatorframework.io" finalizers, other controller's finalizers
	// won't interfere in this check
	if slices.Equal(currentSorted, newFinalizers) {
		return false, nil
	}

	// Get the GVK for this object type
	gvk, err := apiutil.GVKForObject(obj, c.Scheme())
	if err != nil {
		return false, fmt.Errorf("getting object kind: %w", err)
	}

	// Create an unstructured object for server-side apply
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(obj.GetName())
	u.SetNamespace(obj.GetNamespace())
	u.SetFinalizers(newFinalizers)

	// Use server-side apply to update finalizers
	if err := c.Patch(ctx, u, client.Apply, client.ForceOwnership, client.FieldOwner(owner)); err != nil {
		return false, fmt.Errorf("updating finalizers: %w", err)
	}

	// Update the passed object with the new finalizers
	obj.SetFinalizers(u.GetFinalizers())
	obj.SetResourceVersion(u.GetResourceVersion())

	return true, nil
}
