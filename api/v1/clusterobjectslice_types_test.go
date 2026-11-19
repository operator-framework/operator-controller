package v1

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestClusterObjectSliceValidity(t *testing.T) {
	c := newClient(t)
	for i, tc := range []struct {
		name    string
		objects []ClusterObjectSliceEntry
		wantErr string
	}{
		{
			name: "JSON entry",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Namespace: "example", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"example"}}`)},
			},
		},
		{
			name: "cluster scoped entry with default namespace",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "Namespace", Name: "example", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"example"}}`)},
			},
		},
		{
			name: "target API does not need to exist",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "uninstalled.example.com/v1alpha1", Kind: "Widget", Name: "example", Namespace: "example", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{"apiVersion":"uninstalled.example.com/v1alpha1","kind":"Widget","metadata":{"name":"example","namespace":"example"},"spec":{"unknown":{"values":[1,2,3]}}}`)},
			},
		},
		{
			name: "resource specific names are allowed",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole", Name: "system:example", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
		},
		{
			name: "malformed JSON remains opaque",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`not JSON`)},
			},
		},
		{
			name: "malformed gzip remains opaque",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingGzipJSON, Content: []byte{0x1f, 0x8b, 0x00}},
			},
		},
		{
			name: "identity mismatch remains opaque",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"different"}}`)},
			},
		},
		{
			name: "same name in different namespaces",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Namespace: "first", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Namespace: "second", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
		},
		{
			name: "different API versions are distinct envelope keys",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "example.com/v1", Kind: "Widget", Name: "example", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
				{APIVersion: "example.com/v2", Kind: "Widget", Name: "example", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
		},
		{
			name:    "maximum entry count",
			objects: sliceTestObjects(256),
		},
		{
			name:    "too many entries",
			objects: sliceTestObjects(257),
			wantErr: "spec.objects",
		},
		{
			name:    "spec omitted",
			wantErr: "spec",
		},
		{
			name:    "objects empty",
			objects: []ClusterObjectSliceEntry{},
			wantErr: "spec.objects",
		},
		{
			name: "duplicate identities",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Namespace: "example", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Namespace: "example", Encoding: ClusterObjectSliceEncodingGzipJSON, Content: []byte(`different`)},
			},
			wantErr: "Duplicate value",
		},
		{
			name: "duplicate cluster scoped identities",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "Namespace", Name: "example", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
				{APIVersion: "v1", Kind: "Namespace", Name: "example", Namespace: "", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			wantErr: "Duplicate value",
		},
		{
			name: "API version empty",
			objects: []ClusterObjectSliceEntry{
				{Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			wantErr: "apiVersion",
		},
		{
			name: "API version invalid",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "apps//v1", Kind: "Deployment", Name: "example", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			wantErr: "apiVersion",
		},
		{
			name: "kind empty",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			wantErr: "kind",
		},
		{
			name: "kind invalid",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "1ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			wantErr: "kind",
		},
		{
			name: "name empty",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			wantErr: "name",
		},
		{
			name: "name invalid",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "bad/name", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			wantErr: "name",
		},
		{
			name: "namespace invalid",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Namespace: "Not-A-Namespace", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			wantErr: "namespace",
		},
		{
			name: "encoding omitted",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Content: []byte(`{}`)},
			},
			wantErr: "encoding",
		},
		{
			name: "encoding invalid",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: "YAML", Content: []byte(`{}`)},
			},
			wantErr: "encoding",
		},
		{
			name: "content omitted",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON},
			},
			wantErr: "content",
		},
		{
			name: "content empty",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte{}},
			},
			wantErr: "content",
		},
		{
			name: "maximum encoded content length",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(strings.Repeat("x", 786432))},
			},
		},
		{
			name: "encoded content too long",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(strings.Repeat("x", 786433))},
			},
			wantErr: "content",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slice := &ClusterObjectSlice{
				ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("slice-validity-%d", i)},
				Spec:       ClusterObjectSliceSpec{Objects: tc.objects},
			}
			err := c.Create(t.Context(), slice)
			if tc.wantErr != "" {
				require.True(t, apierrors.IsInvalid(err), "expected invalid slice, got: %v", err)
				require.ErrorContains(t, err, tc.wantErr)
				require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(slice), &ClusterObjectSlice{})))
				return
			}
			require.NoError(t, err)
			stored := &ClusterObjectSlice{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(slice), stored))
			require.Equal(t, tc.objects, stored.Spec.Objects)
			require.Empty(t, stored.Namespace)
			t.Cleanup(func() {
				require.NoError(t, client.IgnoreNotFound(c.Delete(context.Background(), stored)))
			})
		})
	}
}

func TestClusterObjectSliceImmutability(t *testing.T) {
	c := newClient(t)
	for i, tc := range []struct {
		name    string
		before  []ClusterObjectSliceEntry
		after   []ClusterObjectSliceEntry
		wantErr bool
	}{
		{
			name: "payload cannot change",
			before: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{"nested":{"values":[1,2]}}`)},
			},
			after: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{"nested":{"values":[1,3]}}`)},
			},
			wantErr: true,
		},
		{
			name: "encoding cannot change",
			before: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			after: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingGzipJSON, Content: []byte(`{}`)},
			},
			wantErr: true,
		},
		{
			name: "identity cannot change",
			before: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			after: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "different", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			wantErr: true,
		},
		{
			name: "entry cannot be added",
			before: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "first", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			after: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "first", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
				{APIVersion: "v1", Kind: "ConfigMap", Name: "second", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			wantErr: true,
		},
		{
			name: "entry cannot be removed",
			before: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "first", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
				{APIVersion: "v1", Kind: "ConfigMap", Name: "second", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			after: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "first", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
			wantErr: true,
		},
		{
			name: "list order has no meaning",
			before: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "first", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`first`)},
				{APIVersion: "v1", Kind: "ConfigMap", Name: "second", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`second`)},
			},
			after: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "second", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`second`)},
				{APIVersion: "v1", Kind: "ConfigMap", Name: "first", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`first`)},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slice := &ClusterObjectSlice{
				ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("slice-immutable-%d", i)},
				Spec:       ClusterObjectSliceSpec{Objects: tc.before},
			}
			require.NoError(t, c.Create(t.Context(), slice))
			t.Cleanup(func() {
				require.NoError(t, client.IgnoreNotFound(c.Delete(context.Background(), slice)))
			})

			slice.Spec.Objects = tc.after
			err := c.Update(t.Context(), slice)
			if tc.wantErr {
				require.True(t, apierrors.IsInvalid(err), "expected immutable-content rejection, got: %v", err)
				require.ErrorContains(t, err, "spec is immutable")
				stored := &ClusterObjectSlice{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(slice), stored))
				require.Equal(t, tc.before, stored.Spec.Objects)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClusterObjectSliceMetadataIsMutable(t *testing.T) {
	c := newClient(t)
	slice := &ClusterObjectSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "slice-mutable-metadata"},
		Spec: ClusterObjectSliceSpec{Objects: []ClusterObjectSliceEntry{
			{APIVersion: "v1", Kind: "Namespace", Name: "example", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
		}},
	}
	require.NoError(t, c.Create(t.Context(), slice))

	slice.Labels = map[string]string{"app.kubernetes.io/name": "example"}
	slice.Annotations = map[string]string{"olm.operatorframework.io/example": "updated"}
	slice.OwnerReferences = []metav1.OwnerReference{
		{APIVersion: GroupVersion.String(), Kind: "ClusterExtension", Name: "example", UID: "example-owner-uid"},
	}
	slice.Finalizers = []string{"olm.operatorframework.io/example"}
	require.NoError(t, c.Update(t.Context(), slice))

	stored := &ClusterObjectSlice{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(slice), stored))
	require.Equal(t, slice.Labels, stored.Labels)
	require.Equal(t, slice.Annotations, stored.Annotations)
	require.Equal(t, slice.OwnerReferences, stored.OwnerReferences)
	require.Equal(t, slice.Finalizers, stored.Finalizers)

	require.NoError(t, c.Delete(t.Context(), stored))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(slice), stored))
	require.False(t, stored.DeletionTimestamp.IsZero())
	stored.Finalizers = nil
	require.NoError(t, c.Update(t.Context(), stored))
}

func TestClusterObjectSliceServerSideDryRun(t *testing.T) {
	c := newClient(t)
	for _, tc := range []struct {
		name    string
		objects []ClusterObjectSliceEntry
		wantErr string
	}{
		{
			name: "slice-valid-dry-run",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "Namespace", Name: "example", Encoding: ClusterObjectSliceEncodingJSON, Content: []byte(`{}`)},
			},
		},
		{
			name: "slice-invalid-dry-run",
			objects: []ClusterObjectSliceEntry{
				{APIVersion: "v1", Kind: "Namespace", Name: "example", Encoding: "Unsupported", Content: []byte(`{}`)},
			},
			wantErr: "encoding",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slice := &ClusterObjectSlice{
				ObjectMeta: metav1.ObjectMeta{Name: tc.name},
				Spec:       ClusterObjectSliceSpec{Objects: tc.objects},
			}
			err := c.Create(t.Context(), slice, client.DryRunAll)
			if tc.wantErr != "" {
				require.True(t, apierrors.IsInvalid(err), "expected invalid dry-run, got: %v", err)
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(slice), &ClusterObjectSlice{})))
		})
	}
}

func TestClusterObjectSliceRejectsInvalidBase64(t *testing.T) {
	c := newClient(t)
	slice := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": GroupVersion.String(),
		"kind":       ClusterObjectSliceKind,
		"metadata":   map[string]any{"name": "slice-invalid-base64"},
		"spec": map[string]any{
			"objects": []any{map[string]any{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"name":       "example",
				"encoding":   "JSON",
				"content":    "not-base64!",
			}},
		},
	}}
	err := c.Create(t.Context(), slice)
	require.True(t, apierrors.IsInvalid(err), "expected invalid base64 rejection, got: %v", err)
	require.ErrorContains(t, err, "content")
}

func sliceTestObjects(count int) []ClusterObjectSliceEntry {
	objects := make([]ClusterObjectSliceEntry, count)
	for i := range objects {
		objects[i] = ClusterObjectSliceEntry{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       fmt.Sprintf("settings-%d", i),
			Encoding:   ClusterObjectSliceEncodingJSON,
			Content:    []byte(`{}`),
		}
	}
	return objects
}
