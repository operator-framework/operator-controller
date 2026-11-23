package steps

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	corev1 "k8s.io/api/core/v1"

	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

func TestHelmReleaseFromSecretRestoresStoredBundleLabels(t *testing.T) {
	// Match the chunked driver's wrapper: release.Release.Labels itself has
	// json:"-", so decoding directly into release.Release silently loses it.
	stored := struct {
		release.Release
		Labels map[string]string `json:"labels"`
	}{
		Release: release.Release{Name: "extension", Manifest: "bundle manifests", Version: 2},
		Labels:  map[string]string{labels.BundleVersionKey: "1.0.1"},
	}
	want := struct {
		name     string
		manifest string
		revision int
		labels   map[string]string
	}{
		name:     "extension",
		manifest: "bundle manifests",
		revision: 2,
		labels:   map[string]string{labels.BundleVersionKey: "1.0.1"},
	}

	data, err := json.Marshal(stored)
	require.NoError(t, err)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err = writer.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	rel, err := helmReleaseFromSecret(&corev1.Secret{Data: map[string][]byte{"chunk": compressed.Bytes()}})
	require.NoError(t, err)
	require.Equal(t, want.name, rel.Name)
	require.Equal(t, want.manifest, rel.Manifest)
	require.Equal(t, want.revision, rel.Version)
	require.Equal(t, want.labels, rel.Labels)
}
