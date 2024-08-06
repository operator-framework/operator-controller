package util_test

import (
	"errors"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/rukpak/util"
)

func TestMergeMaps(t *testing.T) {
	tests := []struct {
		name      string
		maps      []map[string]string
		expectMap map[string]string
	}{
		{
			name:      "no maps",
			maps:      make([]map[string]string, 0),
			expectMap: map[string]string{},
		},
		{
			name:      "two empty maps",
			maps:      []map[string]string{{}, {}},
			expectMap: map[string]string{},
		},
		{
			name:      "simple add",
			maps:      []map[string]string{{"foo": "bar"}, {"bar": "foo"}},
			expectMap: map[string]string{"foo": "bar", "bar": "foo"},
		},
		{
			name:      "subsequent maps overwrite prior",
			maps:      []map[string]string{{"foo": "bar", "bar": "foo"}, {"foo": "foo"}, {"bar": "bar"}},
			expectMap: map[string]string{"foo": "foo", "bar": "bar"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equalf(t, tc.expectMap, util.MergeMaps(tc.maps...), "maps did not merge as expected")
		})
	}
}

// Mock reader for testing that always returns an error when Read is called
type errorReader struct {
	io.Reader
}

func (m errorReader) Read(p []byte) (int, error) {
	return 0, errors.New("Oh no!")
}

func TestManifestObjects(t *testing.T) {
	tests := []struct {
		name          string
		wantErr       bool
		yaml          string
		expectObjects []client.Object
		reader        func(string) io.Reader
	}{
		{
			name:          "nothing",
			wantErr:       false,
			yaml:          "",
			expectObjects: make([]client.Object, 0),
		},
		{
			name:    "happy path",
			wantErr: false,
			yaml: `apiVersion: v1
kind: Service
metadata:
  name: service-a
  namespace: ns-a
spec:
  clusterIP: None
---
apiVersion: v1
kind: Service
metadata:
  name: service-b
  namespace: ns-b
spec:
  clusterIP: 0.0.0.0`,
			expectObjects: []client.Object{
				&corev1.Service{
					TypeMeta:   metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "service-a", Namespace: "ns-a"},
					Spec:       corev1.ServiceSpec{ClusterIP: "None"},
				},
				&corev1.Service{
					TypeMeta:   metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "service-b", Namespace: "ns-b"},
					Spec:       corev1.ServiceSpec{ClusterIP: "0.0.0.0"},
				},
			},
		},
		{
			name:    "invalid yaml error",
			wantErr: true,
			yaml:    "not yaml!",
		},
		{
			name:    "bad reader error",
			wantErr: true,
			yaml: `apiVersion: v1
kind: Service
metadata:
  name: service-a
spec:
  clusterIP: None`,
			reader: func(s string) io.Reader { return errorReader{} },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.reader == nil {
				tc.reader = func(s string) io.Reader { return strings.NewReader(s) }
			}
			objs, err := util.ManifestObjects(tc.reader(tc.yaml), tc.name)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				assert.Equal(t, len(objs), len(tc.expectObjects))
				// Sort the objs by name for easy direct comparison
				sort.Slice(objs, func(i int, j int) bool {
					return objs[i].GetName() < objs[j].GetName()
				})
				if len(objs) > 0 {
					for i, obj := range objs {
						assert.Equal(t, tc.expectObjects[i].GetName(), obj.GetName())
						assert.Equal(t, tc.expectObjects[i].GetNamespace(), obj.GetNamespace())
						assert.Equal(t, tc.expectObjects[i].GetObjectKind().GroupVersionKind(), obj.GetObjectKind().GroupVersionKind())
					}
				}
			}
		})
	}
}
