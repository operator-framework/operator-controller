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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// AddFinalizers adds one or more finalizers to the object using server-side apply.
// If all finalizers already exist, this is a no-op and returns (false, nil).
// Returns (true, nil) if any finalizers were added.
// Note: This function will update the passed object with the server response.
func AddFinalizers(ctx context.Context, owner string, c client.Client, obj client.Object, finalizers ...string) (bool, error) {
	if len(finalizers) == 0 {
		return false, nil
	}

	// Determine which finalizers need to be added
	var toAdd []string
	for _, finalizer := range finalizers {
		if !controllerutil.ContainsFinalizer(obj, finalizer) {
			toAdd = append(toAdd, finalizer)
		}
	}

	if len(toAdd) == 0 {
		return false, nil
	}

	// Sort the finalizers to add for consistent ordering
	sort.Strings(toAdd)

	// Create a copy of the finalizers list to avoid mutating the object
	currentFinalizers := obj.GetFinalizers()
	newFinalizers := make([]string, len(currentFinalizers), len(currentFinalizers)+len(toAdd))
	copy(newFinalizers, currentFinalizers)
	newFinalizers = append(newFinalizers, toAdd...)

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
		return false, fmt.Errorf("adding finalizer: %w", err)
	}

	// Update the passed object with the new finalizers
	obj.SetFinalizers(newFinalizers)
	obj.SetResourceVersion(u.GetResourceVersion())

	return true, nil
}

// RemoveFinalizers removes one or more finalizers from the object using server-side apply.
// If none of the finalizers exist, this is a no-op.
func RemoveFinalizers(ctx context.Context, owner string, c client.Client, obj client.Object, finalizers ...string) error {
	if len(finalizers) == 0 {
		return nil
	}

	// Create a set of finalizers to remove for efficient lookup
	toRemove := sets.New(finalizers...)
	hasAny := false
	for _, finalizer := range finalizers {
		if controllerutil.ContainsFinalizer(obj, finalizer) {
			hasAny = true
		}
	}

	if !hasAny {
		return nil
	}

	// Create a copy of the finalizers list and remove the specified finalizers
	currentFinalizers := obj.GetFinalizers()
	newFinalizers := slices.Clone(currentFinalizers)
	newFinalizers = slices.DeleteFunc(newFinalizers, func(f string) bool {
		return toRemove.Has(f)
	})

	// Get the GVK for this object type
	gvk, err := apiutil.GVKForObject(obj, c.Scheme())
	if err != nil {
		return fmt.Errorf("getting object kind: %w", err)
	}

	// Create an unstructured object for server-side apply
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(obj.GetName())
	u.SetNamespace(obj.GetNamespace())
	u.SetFinalizers(newFinalizers)

	// Use server-side apply to update finalizers
	if err := c.Patch(ctx, u, client.Apply, client.ForceOwnership, client.FieldOwner(owner)); err != nil {
		return fmt.Errorf("removing finalizer: %w", err)
	}

	// Update the passed object with the new finalizers
	obj.SetFinalizers(newFinalizers)
	obj.SetResourceVersion(u.GetResourceVersion())

	return nil
}
