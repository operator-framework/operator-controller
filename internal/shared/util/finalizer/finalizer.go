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
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	FinalizerPrefix = "olm.operatorframework.io/"
)

// EnsureFinalizers sets the FinalizerPrefix finalizers an object using Patch (vs. Update)
// If no finalizers are supplied, all FinalizerPrefix finalizers will be removed from the object.
// If any finalizers are supplied, all other FinalizerPrefix finalizers will be removed and only the supplied ones will remain.
// Returns (true, nil) if the finalizers were changed, (false, nil) if they were already set to the desired value.
// Note: This function will update ONLY the finalizers field of the passed object, not other metadata fields.
func EnsureFinalizers(ctx context.Context, owner string, c client.Client, obj client.Object, finalizers ...string) (bool, error) {
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

	// Add any other, non-FinalizerPrefix, finalizers to the newFinalizer list
	currentFinalizers := obj.GetFinalizers()
	for _, f := range currentFinalizers {
		if !strings.HasPrefix(f, FinalizerPrefix) {
			newFinalizers = append(newFinalizers, f)
		}
	}
	// Sort the desired finalizers for consistent ordering
	sort.Strings(newFinalizers)

	// Check if the current finalizers already match the desired state (newFinalizers)
	currentSorted := slices.Clone(currentFinalizers)
	if currentSorted == nil {
		currentSorted = []string{}
	}
	sort.Strings(currentSorted)

	// Compare the current list with the desired newFinalizers
	if slices.Equal(currentSorted, newFinalizers) {
		return false, nil
	}

	patch := map[string]any{
		"metadata": map[string]any{
			"resourceVersion": obj.GetResourceVersion(),
			"finalizers":      newFinalizers,
		},
	}

	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return false, fmt.Errorf("marshalling patch to ensure finalizers: %w", err)
	}

	// Create a copy to use for patching. We patch the copy to avoid having the server's
	// patch response overwrite the original object with potentially changed metadata fields
	// (like annotations) that the caller didn't intend to modify.
	objCopy := obj.DeepCopyObject().(client.Object)

	// Use patch to update finalizers on the server
	if err := c.Patch(ctx, objCopy, client.RawPatch(types.MergePatchType, patchJSON)); err != nil {
		return false, fmt.Errorf("updating finalizers: %w", err)
	}

	// Update the finalizers and resource version in the original object to reflect the change.
	// The resource version must be updated to avoid conflicts with subsequent operations.
	obj.SetFinalizers(objCopy.GetFinalizers())
	obj.SetResourceVersion(objCopy.GetResourceVersion())

	return true, nil
}
