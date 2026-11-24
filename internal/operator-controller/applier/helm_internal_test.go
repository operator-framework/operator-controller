package applier

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"

	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

func TestPostrenderer_ReplacesLegacyLabels(t *testing.T) {
	t.Parallel()
	post := &postrenderer{labels: map[string]string{
		labels.ClusterExtensionNameKey: "my-extension",
	}}
	rendered, err := post.Run(bytes.NewBufferString(`apiVersion: v1
kind: ConfigMap
metadata:
  name: managed
  labels:
    olm.operatorframework.io/owner-kind: ClusterExtension
    olm.operatorframework.io/owner-name: old-extension
    example.com/custom: keep
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: new
`))
	require.NoError(t, err)
	objects, err := (HelmReleaseToObjectsConverter{}).GetObjectsFromRelease(&release.Release{Manifest: rendered.String()})
	require.NoError(t, err)
	require.Len(t, objects, 2)
	assert.Equal(t, map[string]string{
		labels.ClusterExtensionNameKey: "my-extension",
		"example.com/custom":           "keep",
	}, objects[0].GetLabels())
	assert.Equal(t, map[string]string{labels.ClusterExtensionNameKey: "my-extension"}, objects[1].GetLabels())
}
