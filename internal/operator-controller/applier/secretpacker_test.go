package applier

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

func TestSecretPacker_Pack(t *testing.T) {
	packer := SecretPacker{
		RevisionName:    "my-ext-3",
		OwnerName:       "my-ext",
		SystemNamespace: "olmv1-system",
	}

	t.Run("empty phases produce no Secrets", func(t *testing.T) {
		result, err := packer.Pack(nil)
		require.NoError(t, err)
		assert.Empty(t, result.Secrets)
		assert.Empty(t, result.Refs)
	})

	t.Run("single object packs into one Secret", func(t *testing.T) {
		phases := []ocv1.ClusterExtensionRevisionPhase{{
			Name: "deploy",
			Objects: []ocv1.ClusterExtensionRevisionObject{{
				Object: testConfigMap("test-cm", "default"),
			}},
		}}

		result, err := packer.Pack(phases)
		require.NoError(t, err)

		require.Len(t, result.Secrets, 1)
		assert.True(t, strings.HasPrefix(result.Secrets[0].Name, "my-ext-3-"), "Secret name should be content-addressable with revision prefix")
		assert.Equal(t, "olmv1-system", result.Secrets[0].Namespace)
		assert.True(t, *result.Secrets[0].Immutable)
		assert.Equal(t, "my-ext-3", result.Secrets[0].Labels[labels.RevisionNameKey])
		assert.Equal(t, "my-ext", result.Secrets[0].Labels[labels.OwnerNameKey])

		ref, ok := result.Refs[[2]int{0, 0}]
		require.True(t, ok)
		assert.Equal(t, result.Secrets[0].Name, ref.Name)
		assert.Equal(t, "olmv1-system", ref.Namespace)

		// Verify the key is a valid base64url hash (43 chars).
		assert.Len(t, ref.Key, 43)

		// Verify the data at the key deserializes back to the same object.
		data, ok := result.Secrets[0].Data[ref.Key]
		require.True(t, ok)
		var roundtrip map[string]interface{}
		require.NoError(t, json.Unmarshal(data, &roundtrip))
		assert.Equal(t, "ConfigMap", roundtrip["kind"])
	})

	t.Run("multiple objects fit in one Secret", func(t *testing.T) {
		phases := []ocv1.ClusterExtensionRevisionPhase{{
			Name: "deploy",
			Objects: []ocv1.ClusterExtensionRevisionObject{
				{Object: testConfigMap("cm-1", "default")},
				{Object: testConfigMap("cm-2", "default")},
			},
		}}

		result, err := packer.Pack(phases)
		require.NoError(t, err)
		require.Len(t, result.Secrets, 1)
		assert.Len(t, result.Secrets[0].Data, 2)
		assert.Len(t, result.Refs, 2)
	})

	t.Run("objects across multiple phases", func(t *testing.T) {
		phases := []ocv1.ClusterExtensionRevisionPhase{
			{
				Name:    "crds",
				Objects: []ocv1.ClusterExtensionRevisionObject{{Object: testConfigMap("crd-1", "")}},
			},
			{
				Name:    "deploy",
				Objects: []ocv1.ClusterExtensionRevisionObject{{Object: testConfigMap("deploy-1", "ns")}},
			},
		}

		result, err := packer.Pack(phases)
		require.NoError(t, err)

		// Both objects fit in one Secret.
		require.Len(t, result.Secrets, 1)

		// Refs point correctly to each phase/object.
		ref0, ok := result.Refs[[2]int{0, 0}]
		require.True(t, ok)
		ref1, ok := result.Refs[[2]int{1, 0}]
		require.True(t, ok)
		assert.Equal(t, ref0.Name, ref1.Name)  // same Secret
		assert.NotEqual(t, ref0.Key, ref1.Key) // different keys
	})

	t.Run("deterministic: same input produces same output", func(t *testing.T) {
		phases := []ocv1.ClusterExtensionRevisionPhase{{
			Name: "deploy",
			Objects: []ocv1.ClusterExtensionRevisionObject{
				{Object: testConfigMap("cm-1", "default")},
			},
		}}

		result1, err := packer.Pack(phases)
		require.NoError(t, err)
		result2, err := packer.Pack(phases)
		require.NoError(t, err)

		require.Len(t, result1.Secrets, 1)
		require.Len(t, result2.Secrets, 1)
		assert.Equal(t, result1.Refs, result2.Refs)
	})

	t.Run("skips ref-only objects", func(t *testing.T) {
		phases := []ocv1.ClusterExtensionRevisionPhase{{
			Name: "deploy",
			Objects: []ocv1.ClusterExtensionRevisionObject{
				{Ref: ocv1.ObjectSourceRef{Name: "existing-secret", Namespace: "ns", Key: "somekey"}},
			},
		}}

		result, err := packer.Pack(phases)
		require.NoError(t, err)
		assert.Empty(t, result.Secrets)
		assert.Empty(t, result.Refs)
	})

	t.Run("large object gets gzipped", func(t *testing.T) {
		// Create a large ConfigMap with data exceeding gzipThreshold (800 KiB).
		largeObj := testConfigMap("large-cm", "default")
		largeObj.Object["data"] = map[string]interface{}{
			// Repetitive data compresses well with gzip.
			"bigkey": strings.Repeat("a", gzipThreshold+1),
		}

		phases := []ocv1.ClusterExtensionRevisionPhase{{
			Name:    "deploy",
			Objects: []ocv1.ClusterExtensionRevisionObject{{Object: largeObj}},
		}}

		result, err := packer.Pack(phases)
		require.NoError(t, err)
		require.Len(t, result.Secrets, 1)

		ref := result.Refs[[2]int{0, 0}]
		data := result.Secrets[0].Data[ref.Key]

		// Verify the stored data is gzip-compressed (magic bytes 0x1f 0x8b).
		require.GreaterOrEqual(t, len(data), 2)
		assert.Equal(t, byte(0x1f), data[0])
		assert.Equal(t, byte(0x8b), data[1])

		// Verify we can decompress it.
		reader, err := gzip.NewReader(bytes.NewReader(data))
		require.NoError(t, err)
		defer reader.Close()
	})

	t.Run("duplicate content objects share key and do not inflate size", func(t *testing.T) {
		// Two identical objects should produce the same content hash key.
		// The second occurrence must not double-count the size.
		phases := []ocv1.ClusterExtensionRevisionPhase{{
			Name: "deploy",
			Objects: []ocv1.ClusterExtensionRevisionObject{
				{Object: testConfigMap("same-cm", "default")},
				{Object: testConfigMap("same-cm", "default")},
			},
		}}

		result, err := packer.Pack(phases)
		require.NoError(t, err)
		require.Len(t, result.Secrets, 1)
		// Only one data entry despite two objects.
		assert.Len(t, result.Secrets[0].Data, 1)
		// Both positions get refs.
		assert.Len(t, result.Refs, 2)
		// Both refs point to the same key.
		assert.Equal(t, result.Refs[[2]int{0, 0}].Key, result.Refs[[2]int{0, 1}].Key)
	})

	t.Run("key is SHA-256 base64url", func(t *testing.T) {
		obj := testConfigMap("test-cm", "default")
		rawData, err := json.Marshal(obj.Object)
		require.NoError(t, err)

		expectedHash := sha256.Sum256(rawData)
		expectedKey := base64.RawURLEncoding.EncodeToString(expectedHash[:])

		phases := []ocv1.ClusterExtensionRevisionPhase{{
			Name:    "deploy",
			Objects: []ocv1.ClusterExtensionRevisionObject{{Object: obj}},
		}}
		result, err := packer.Pack(phases)
		require.NoError(t, err)

		ref := result.Refs[[2]int{0, 0}]
		assert.Equal(t, expectedKey, ref.Key)
	})
}

func testConfigMap(name, namespace string) unstructured.Unstructured {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name": name,
		},
	}
	if namespace != "" {
		obj["metadata"].(map[string]interface{})["namespace"] = namespace
	}
	return unstructured.Unstructured{Object: obj}
}
