package action

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/operator-framework/helm-operator-plugins/pkg/storage"
)

const (
	// maxCompressedReleaseSize is the maximum total size in bytes of a
	// gzip-compressed Helm release that the chunked storage driver can store
	// (ChunkSize * MaxWriteChunks). It is tested for exact equality with the
	// config so that any config increase forces this const to be raised too.
	// This value must never decrease: lowering it would cause previously-storable
	// releases to fail.
	maxCompressedReleaseSize = (1024 - 8) * 1024 * 11 // 1016 KB * 11 chunks = 11,176 KB

	// minMaxWriteChunks is tested for exact equality with MaxWriteChunks so
	// that any config increase forces this const to be raised too. This value
	// must never decrease: lowering it would cause previously-storable releases
	// to fail.
	minMaxWriteChunks = 11

	// minMaxReadChunks is tested for exact equality with MaxReadChunks so
	// that any config increase forces this const to be raised too. This value
	// must never decrease: lowering it would make previously-stored releases
	// unreadable.
	minMaxReadChunks = 11
)

func TestChunkedSecretsConfigTotalCapacity(t *testing.T) {
	assert.Equal(t, maxCompressedReleaseSize, chunkedSecretsConfig.ChunkSize*chunkedSecretsConfig.MaxWriteChunks,
		"ChunkSize * MaxWriteChunks must equal maxCompressedReleaseSize")
}

func TestChunkedSecretsConfigMaxWriteChunks(t *testing.T) {
	assert.Equal(t, minMaxWriteChunks, chunkedSecretsConfig.MaxWriteChunks,
		"MaxWriteChunks changed — update minMaxWriteChunks to match (it must never decrease)")
}

func TestChunkedSecretsConfigMaxReadChunks(t *testing.T) {
	assert.Equal(t, minMaxReadChunks, chunkedSecretsConfig.MaxReadChunks,
		"MaxReadChunks changed — update minMaxReadChunks to match (it must never decrease)")
	assert.GreaterOrEqual(t, chunkedSecretsConfig.MaxReadChunks, chunkedSecretsConfig.MaxWriteChunks,
		"MaxReadChunks must be >= MaxWriteChunks so any written release can be read back")
}

func TestChunkedSecretsMaxCapacityRelease(t *testing.T) {
	// Regression test: stores a release through the real chunked storage driver
	// that fills all MaxWriteChunks chunks, reads it back, and verifies the
	// first MaxWriteChunks-1 chunks are exactly ChunkSize bytes. This proves
	// the configured ChunkSize fits within the Kubernetes 1MB Secret data limit
	// end-to-end against a real API server.

	chunkSize := chunkedSecretsConfig.ChunkSize
	maxChunks := chunkedSecretsConfig.MaxWriteChunks

	// Use a large incompressible payload to guarantee all chunks are used.
	// Raw []byte in Config is base64-encoded by json.Marshal, giving full
	// 8-bit entropy. The base64 expansion (~33%) and gzip compression roughly
	// cancel out at a ~1.004 ratio, so ChunkSize*10 raw bytes compresses to
	// just over 10 chunks, requiring all 11.
	rel := &release.Release{
		Name:    "max-capacity-test",
		Version: 1,
		Config:  map[string]any{"payload": deterministicPayload(chunkSize * (maxChunks - 1))},
		Info:    &release.Info{Status: release.StatusDeployed},
	}

	secretsClient := clientcorev1.NewForConfigOrDie(cfg).Secrets("default")
	drv := storage.NewChunkedSecrets(secretsClient, "test-owner", chunkedSecretsConfig)

	key := fmt.Sprintf("sh.helm.release.v1.%s.v%d", rel.Name, rel.Version)
	require.NoError(t, drv.Create(key, rel))

	// Verify round-trip
	actual, err := drv.Get(key)
	require.NoError(t, err)
	assert.Equal(t, rel.Name, actual.Name)
	assert.Equal(t, rel.Version, actual.Version)

	// Collect secrets and verify chunk sizes using the extraChunks field
	// from the index Secret to determine ordering.
	allSecrets, err := secretsClient.List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)

	secretsByName := map[string][]byte{}
	var indexChunkData []byte
	var extraChunkNames []string
	for _, s := range allSecrets.Items {
		switch s.Type {
		case storage.SecretTypeChunkedIndex:
			indexChunkData = s.Data["chunk"]
			require.NoError(t, json.Unmarshal(s.Data["extraChunks"], &extraChunkNames))
		case storage.SecretTypeChunkedChunk:
			secretsByName[s.Name] = s.Data["chunk"]
		}
	}

	require.NotNil(t, indexChunkData, "index Secret must exist")
	require.Lenf(t, extraChunkNames, maxChunks-1,
		"release must use all %d chunks", maxChunks)

	// The first 10 chunks (index + 9 extras) must be exactly ChunkSize.
	// The last chunk may be smaller since it holds the remainder.
	assert.Lenf(t, indexChunkData, chunkSize,
		"chunk 1/%d (index) must be exactly ChunkSize", maxChunks)
	for i, name := range extraChunkNames[:maxChunks-2] {
		chunkData, ok := secretsByName[name]
		require.True(t, ok, "chunk Secret %q not found", name)
		assert.Lenf(t, chunkData, chunkSize,
			"chunk %d/%d must be exactly ChunkSize", i+2, maxChunks)
	}

	// The last chunk just needs to be non-empty.
	lastName := extraChunkNames[maxChunks-2]
	lastChunk, ok := secretsByName[lastName]
	require.True(t, ok, "last chunk Secret %q not found", lastName)
	assert.NotEmpty(t, lastChunk, "last chunk must be non-empty")

	_, err = drv.Delete(key)
	require.NoError(t, err)
}

func deterministicPayload(n int) []byte {
	b := make([]byte, 0, n)
	h := sha256.Sum256([]byte("deterministicPayload"))
	for len(b) < n {
		b = append(b, h[:]...)
		h = sha256.Sum256(h[:])
	}
	return b[:n]
}
