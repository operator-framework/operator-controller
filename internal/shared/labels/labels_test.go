package labels_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

func TestForClusterExtension(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		existing map[string]string
		want     map[string]string
	}{
		{
			name: "new resource",
			want: map[string]string{labels.ClusterExtensionNameKey: "my-extension"},
		},
		{
			name: "legacy labels are replaced and unrelated labels retained",
			existing: map[string]string{
				"olm.operatorframework.io/owner-kind": "ClusterExtension",
				"olm.operatorframework.io/owner-name": "old-extension",
				"example.com/custom":                  "keep",
			},
			want: map[string]string{
				labels.ClusterExtensionNameKey: "my-extension",
				"example.com/custom":           "keep",
			},
		},
		{
			name: "current identity overrides conflicting labels",
			existing: map[string]string{
				labels.ClusterExtensionNameKey:        "other-extension",
				"olm.operatorframework.io/owner-name": "other-extension",
			},
			want: map[string]string{labels.ClusterExtensionNameKey: "my-extension"},
		},
		{
			name:     "already migrated resource",
			existing: map[string]string{labels.ClusterExtensionNameKey: "my-extension"},
			want:     map[string]string{labels.ClusterExtensionNameKey: "my-extension"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := maps.Clone(tc.existing)
			got := labels.ForClusterExtension(tc.existing, "my-extension")
			assert.Equal(t, tc.want, got)
			assert.Equal(t, before, tc.existing, "must not mutate cached metadata")
			assert.Equal(t, got, labels.ForClusterExtension(got, "my-extension"), "must be idempotent")
		})
	}
}
