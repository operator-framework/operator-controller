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

package finalizer_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/operator-framework/operator-controller/internal/shared/util/finalizer"
)

func TestEnsureFinalizers(t *testing.T) {
	const (
		testOwner = "test-owner"
		testNS    = "test-namespace"
		testName  = "test-configmap"
	)

	tests := []struct {
		name              string
		initialFinalizers []string
		newFinalizers     []string
		wantChanged       bool
		wantFinalizers    []string
		wantErr           bool
	}{
		{
			name:              "add single finalizer to empty object",
			initialFinalizers: nil,
			newFinalizers:     []string{finalizer.FinalizerPrefix + "test"},
			wantChanged:       true,
			wantFinalizers:    []string{finalizer.FinalizerPrefix + "test"},
		},
		{
			name:              "add multiple finalizers to empty object",
			initialFinalizers: nil,
			newFinalizers:     []string{finalizer.FinalizerPrefix + "test1", finalizer.FinalizerPrefix + "test2"},
			wantChanged:       true,
			wantFinalizers:    []string{finalizer.FinalizerPrefix + "test1", finalizer.FinalizerPrefix + "test2"},
		},
		{
			name:              "finalizers already match - no change",
			initialFinalizers: []string{finalizer.FinalizerPrefix + "test"},
			newFinalizers:     []string{finalizer.FinalizerPrefix + "test"},
			wantChanged:       false,
			wantFinalizers:    []string{finalizer.FinalizerPrefix + "test"},
		},
		{
			name:              "finalizers already match (multiple) - no change",
			initialFinalizers: []string{finalizer.FinalizerPrefix + "test1", finalizer.FinalizerPrefix + "test2"},
			newFinalizers:     []string{finalizer.FinalizerPrefix + "test1", finalizer.FinalizerPrefix + "test2"},
			wantChanged:       false,
			wantFinalizers:    []string{finalizer.FinalizerPrefix + "test1", finalizer.FinalizerPrefix + "test2"},
		},
		{
			name:              "finalizers match but different order - no change",
			initialFinalizers: []string{finalizer.FinalizerPrefix + "test2", finalizer.FinalizerPrefix + "test1"},
			newFinalizers:     []string{finalizer.FinalizerPrefix + "test1", finalizer.FinalizerPrefix + "test2"},
			wantChanged:       false,
			// When no change happens, the object is not patched, so finalizers remain in their original order
			wantFinalizers: []string{finalizer.FinalizerPrefix + "test2", finalizer.FinalizerPrefix + "test1"},
		},
		{
			name:              "remove all FinalizerPrefix finalizers",
			initialFinalizers: []string{finalizer.FinalizerPrefix + "test1", finalizer.FinalizerPrefix + "test2"},
			newFinalizers:     []string{},
			wantChanged:       true,
			wantFinalizers:    []string{},
		},
		{
			name:              "remove all FinalizerPrefix finalizers with nil",
			initialFinalizers: []string{finalizer.FinalizerPrefix + "test"},
			newFinalizers:     nil,
			wantChanged:       true,
			wantFinalizers:    []string{},
		},
		{
			name:              "preserve non-FinalizerPrefix finalizers",
			initialFinalizers: []string{"other.io/finalizer", finalizer.FinalizerPrefix + "test"},
			newFinalizers:     []string{},
			wantChanged:       true,
			wantFinalizers:    []string{"other.io/finalizer"},
		},
		{
			name:              "preserve non-FinalizerPrefix finalizers and add new ones",
			initialFinalizers: []string{"other.io/finalizer"},
			newFinalizers:     []string{finalizer.FinalizerPrefix + "test"},
			wantChanged:       true,
			wantFinalizers:    []string{finalizer.FinalizerPrefix + "test", "other.io/finalizer"},
		},
		{
			name:              "replace FinalizerPrefix finalizers while preserving others",
			initialFinalizers: []string{"other.io/finalizer", finalizer.FinalizerPrefix + "old"},
			newFinalizers:     []string{finalizer.FinalizerPrefix + "new"},
			wantChanged:       true,
			wantFinalizers:    []string{finalizer.FinalizerPrefix + "new", "other.io/finalizer"},
		},
		{
			name:              "multiple non-FinalizerPrefix finalizers preserved and sorted",
			initialFinalizers: []string{"z.io/finalizer", "a.io/finalizer", finalizer.FinalizerPrefix + "test"},
			newFinalizers:     []string{finalizer.FinalizerPrefix + "new"},
			wantChanged:       true,
			wantFinalizers:    []string{"a.io/finalizer", finalizer.FinalizerPrefix + "new", "z.io/finalizer"},
		},
		{
			name:              "no change when object already has correct finalizers with others",
			initialFinalizers: []string{finalizer.FinalizerPrefix + "test", "other.io/finalizer"},
			newFinalizers:     []string{finalizer.FinalizerPrefix + "test"},
			wantChanged:       false,
			wantFinalizers:    []string{finalizer.FinalizerPrefix + "test", "other.io/finalizer"},
		},
		{
			name:              "update existing FinalizerPrefix finalizer",
			initialFinalizers: []string{finalizer.FinalizerPrefix + "old"},
			newFinalizers:     []string{finalizer.FinalizerPrefix + "new"},
			wantChanged:       true,
			wantFinalizers:    []string{finalizer.FinalizerPrefix + "new"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:            testName,
					Namespace:       testNS,
					Finalizers:      tt.initialFinalizers,
					ResourceVersion: "1",
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(cm).
				WithInterceptorFuncs(interceptor.Funcs{
					Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						// Parse the patch to validate structure
						patchBytes, err := patch.Data(obj)
						require.NoError(t, err)

						var patchMap map[string]interface{}
						require.NoError(t, json.Unmarshal(patchBytes, &patchMap))

						// Verify patch structure
						metadata, ok := patchMap["metadata"].(map[string]interface{})
						require.True(t, ok, "patch should contain metadata")

						finalizers, ok := metadata["finalizers"]
						require.True(t, ok, "patch metadata should contain finalizers")

						// Update the object with new finalizers and increment resource version
						// to simulate what the API server does
						if finalizerSlice, ok := finalizers.([]interface{}); ok {
							stringFinalizers := make([]string, len(finalizerSlice))
							for i, f := range finalizerSlice {
								stringFinalizers[i] = f.(string)
							}
							obj.SetFinalizers(stringFinalizers)
							// Simulate API server incrementing resourceVersion
							obj.SetResourceVersion("2")
						}

						return nil
					},
				}).
				Build()

			ctx := context.Background()
			changed, err := finalizer.EnsureFinalizers(ctx, testOwner, fakeClient, cm, tt.newFinalizers...)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, changed, "unexpected changed value")
			assert.Equal(t, tt.wantFinalizers, cm.GetFinalizers(), "unexpected finalizers")
		})
	}
}

func TestEnsureFinalizers_PanicOnInvalidPrefix(t *testing.T) {
	tests := []struct {
		name       string
		finalizers []string
		wantPanic  bool
	}{
		{
			name:       "valid finalizer with correct prefix",
			finalizers: []string{finalizer.FinalizerPrefix + "test"},
			wantPanic:  false,
		},
		{
			name:       "invalid finalizer without prefix",
			finalizers: []string{"test"},
			wantPanic:  true,
		},
		{
			name:       "invalid finalizer with wrong prefix",
			finalizers: []string{"other.io/test"},
			wantPanic:  true,
		},
		{
			name:       "mix of valid and invalid finalizers",
			finalizers: []string{finalizer.FinalizerPrefix + "test", "invalid"},
			wantPanic:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test",
					Namespace:       "test",
					ResourceVersion: "1",
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(cm).
				Build()

			test := func() {
				_, _ = finalizer.EnsureFinalizers(context.Background(), "test", fakeClient, cm, tt.finalizers...)
			}

			if tt.wantPanic {
				require.Panics(t, test)
			} else {
				require.NotPanics(t, test)
			}
		})
	}
}

func TestEnsureFinalizers_FinalizersSorting(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test",
			Namespace:       "test",
			Finalizers:      []string{},
			ResourceVersion: "1",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cm).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				patchBytes, err := patch.Data(obj)
				require.NoError(t, err)

				var patchMap map[string]interface{}
				require.NoError(t, json.Unmarshal(patchBytes, &patchMap))

				metadata := patchMap["metadata"].(map[string]interface{})
				finalizers := metadata["finalizers"]

				if finalizerSlice, ok := finalizers.([]interface{}); ok {
					stringFinalizers := make([]string, len(finalizerSlice))
					for i, f := range finalizerSlice {
						stringFinalizers[i] = f.(string)
					}
					obj.SetFinalizers(stringFinalizers)
				}

				return nil
			},
		}).
		Build()

	ctx := context.Background()

	// Add finalizers in unsorted order
	unsortedFinalizers := []string{
		finalizer.FinalizerPrefix + "zebra",
		finalizer.FinalizerPrefix + "apple",
		finalizer.FinalizerPrefix + "banana",
	}

	changed, err := finalizer.EnsureFinalizers(ctx, "test", fakeClient, cm, unsortedFinalizers...)
	require.NoError(t, err)
	assert.True(t, changed)

	// Verify finalizers are sorted
	expectedFinalizers := []string{
		finalizer.FinalizerPrefix + "apple",
		finalizer.FinalizerPrefix + "banana",
		finalizer.FinalizerPrefix + "zebra",
	}
	assert.Equal(t, expectedFinalizers, cm.GetFinalizers())
}

func TestEnsureFinalizers_EmptyInitialFinalizers(t *testing.T) {
	tests := []struct {
		name              string
		initialFinalizers []string
		newFinalizers     []string
		wantChanged       bool
	}{
		{
			name:              "nil to empty slice",
			initialFinalizers: nil,
			newFinalizers:     []string{},
			wantChanged:       false,
		},
		{
			name:              "empty slice to nil",
			initialFinalizers: []string{},
			newFinalizers:     nil,
			wantChanged:       false,
		},
		{
			name:              "empty slice to empty slice",
			initialFinalizers: []string{},
			newFinalizers:     []string{},
			wantChanged:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test",
					Namespace:       "test",
					Finalizers:      tt.initialFinalizers,
					ResourceVersion: "1",
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(cm).
				Build()

			ctx := context.Background()
			changed, err := finalizer.EnsureFinalizers(ctx, "test", fakeClient, cm, tt.newFinalizers...)

			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, changed)
		})
	}
}

func TestEnsureFinalizers_PatchError(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test",
			Namespace:       "test",
			ResourceVersion: "1",
		},
	}

	// Create a client that will fail on patch
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cm).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				return assert.AnError
			},
		}).
		Build()

	ctx := context.Background()
	changed, err := finalizer.EnsureFinalizers(ctx, "test", fakeClient, cm, finalizer.FinalizerPrefix+"test")

	require.Error(t, err)
	assert.False(t, changed)
	assert.Contains(t, err.Error(), "updating finalizers")
}
