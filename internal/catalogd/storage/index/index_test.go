package index

import (
	"bytes"
	"encoding/json"
	"io"
	"iter"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

func metasSeq(metas []*declcfg.Meta) iter.Seq2[*declcfg.Meta, error] {
	return func(yield func(*declcfg.Meta, error) bool) {
		for _, meta := range metas {
			if !yield(meta, nil) {
				return
			}
		}
	}
}

func TestIndexCreation(t *testing.T) {
	// Create test Meta objects
	metas := []*declcfg.Meta{
		{
			Schema:  "olm.package",
			Package: "test",
			Name:    "test-package",
			Blob:    []byte(`{"test": "data1"}`),
		},
		{
			Schema:  "olm.bundle",
			Package: "test",
			Name:    "test-bundle",
			Blob:    []byte(`{"test": "data2"}`),
		},
	}

	// Create index
	idx, err := newIndex(t.Context(), metasSeq(metas))
	require.NoError(t, err)

	// Verify schema index
	require.Len(t, idx.BySchema, 2, "Expected 2 schema entries, got %d", len(idx.BySchema))
	require.Len(t, idx.BySchema["olm.package"], 1, "Expected 1 olm.package entry, got %d", len(idx.BySchema["olm.package"]))
	require.Len(t, idx.BySchema["olm.bundle"], 1, "Expected 1 olm.bundle entry, got %d", len(idx.BySchema["olm.bundle"]))

	// Verify package index
	require.Len(t, idx.ByPackage["test"], 2, "Expected 2 package entries, got %d", len(idx.ByPackage))

	// Verify name index
	require.Len(t, idx.ByName["test-package"], 1, "Expected 1 entry for name 'test-package', got %d", len(idx.ByName["test-package"]))
	require.Len(t, idx.ByName["test-bundle"], 1, "Expected 1 entry for name 'test-bundle', got %d", len(idx.ByName["test-bundle"]))
}

func TestIndexGet(t *testing.T) {
	// Test data structure that represents a catalog
	metas := []*declcfg.Meta{
		{
			// Package definition
			Schema: "olm.package",
			Name:   "test-package",
			Blob: createBlob(t, map[string]interface{}{
				"schema":         "olm.package",
				"name":           "test-package",
				"defaultChannel": "stable-v6.x",
			}),
		},
		{
			// First channel (stable-5.x)
			Schema:  "olm.channel",
			Package: "test-package",
			Name:    "stable-5.x",
			Blob: createBlob(t, map[string]interface{}{
				"schema":  "olm.channel",
				"name":    "stable-5.x",
				"package": "test-package",
				"entries": []map[string]interface{}{
					{"name": "test-bunble.v5.0.3"},
					{"name": "test-bundle.v5.0.4", "replaces": "test-bundle.v5.0.3"},
				},
			}),
		},
		{
			// Second channel (stable-v6.x)
			Schema:  "olm.channel",
			Package: "test-package",
			Name:    "stable-v6.x",
			Blob: createBlob(t, map[string]interface{}{
				"schema":  "olm.channel",
				"name":    "stable-v6.x",
				"package": "test-package",
				"entries": []map[string]interface{}{
					{"name": "test-bundle.v6.0.0", "skipRange": "<6.0.0"},
				},
			}),
		},
		{
			// Bundle v5.0.3
			Schema:  "olm.bundle",
			Package: "test-package",
			Name:    "test-bundle.v5.0.3",
			Blob: createBlob(t, map[string]interface{}{
				"schema":  "olm.bundle",
				"name":    "test-bundle.v5.0.3",
				"package": "test-package",
				"image":   "test-image@sha256:a5d4f",
				"properties": []map[string]interface{}{
					{
						"type": "olm.package",
						"value": map[string]interface{}{
							"packageName": "test-package",
							"version":     "5.0.3",
						},
					},
				},
			}),
		},
		{
			// Bundle v5.0.4
			Schema:  "olm.bundle",
			Package: "test-package",
			Name:    "test-bundle.v5.0.4",
			Blob: createBlob(t, map[string]interface{}{
				"schema":  "olm.bundle",
				"name":    "test-bundle.v5.0.4",
				"package": "test-package",
				"image":   "test-image@sha256:f4233",
				"properties": []map[string]interface{}{
					{
						"type": "olm.package",
						"value": map[string]interface{}{
							"packageName": "test-package",
							"version":     "5.0.4",
						},
					},
				},
			}),
		},
		{
			// Bundle v6.0.0
			Schema:  "olm.bundle",
			Package: "test-package",
			Name:    "test-bundle.v6.0.0",
			Blob: createBlob(t, map[string]interface{}{
				"schema":  "olm.bundle",
				"name":    "test-bundle.v6.0.0",
				"package": "test-package",
				"image":   "test-image@sha256:d3016b",
				"properties": []map[string]interface{}{
					{
						"type": "olm.package",
						"value": map[string]interface{}{
							"packageName": "test-package",
							"version":     "6.0.0",
						},
					},
				},
			}),
		},
	}

	idx, err := newIndex(t.Context(), metasSeq(metas))
	require.NoError(t, err)

	// Create a reader from the metas
	var combinedBlob bytes.Buffer
	for _, meta := range metas {
		combinedBlob.Write(meta.Blob)
	}
	fullData := bytes.NewReader(combinedBlob.Bytes())

	tests := []struct {
		name        string
		schema      string
		packageName string
		blobName    string
		wantCount   int
		validate    func(t *testing.T, entry map[string]interface{})
	}{
		{
			name:      "filter by schema - olm.package",
			schema:    "olm.package",
			wantCount: 1,
			validate: func(t *testing.T, entry map[string]interface{}) {
				if entry["schema"] != "olm.package" {
					t.Errorf("Expected olm.package schema blob got %v", entry["schema"])
				}
			},
		},
		{
			name:      "filter by schema - olm.channel",
			schema:    "olm.channel",
			wantCount: 2,
			validate: func(t *testing.T, entry map[string]interface{}) {
				if entry["schema"] != "olm.channel" {
					t.Errorf("Expected olm.channel schema blob got %v", entry["schema"])
				}
			},
		},
		{
			name:      "filter by schema - olm.bundle",
			schema:    "olm.bundle",
			wantCount: 3,
			validate: func(t *testing.T, entry map[string]interface{}) {
				if entry["schema"] != "olm.bundle" {
					t.Errorf("Expected olm.bundle schema blob got %v", entry["schema"])
				}
			},
		},
		{
			name:        "filter by package",
			packageName: "test-package",
			wantCount:   5,
			validate: func(t *testing.T, entry map[string]interface{}) {
				if entry["package"] != "test-package" {
					t.Errorf("Expected blobs with package name test-package, got blob with package name %v", entry["package"])
				}
			},
		},
		{
			name:      "filter by specific bundle name",
			blobName:  "test-bundle.v5.0.3",
			wantCount: 1,
			validate: func(t *testing.T, entry map[string]interface{}) {
				if entry["schema"] != "olm.bundle" && entry["name"] != "test-bundle.v5.0.3" {
					t.Errorf("Expected blob with schema=olm.bundle and name=test-bundle.v5.0.3, got %v", entry)
				}
			},
		},
		{
			name:        "filter by schema and package",
			schema:      "olm.bundle",
			packageName: "test-package",
			wantCount:   3,
			validate: func(t *testing.T, entry map[string]interface{}) {
				if entry["schema"] != "olm.bundle" && entry["package"] != "test-package" {
					t.Errorf("Expected blob with schema=olm.bundle and package=test-package, got %v", entry)
				}
			},
		},
		{
			name:        "no matches",
			schema:      "non.existent",
			packageName: "not-found",
			wantCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := idx.get(fullData, tt.schema, tt.packageName, tt.blobName)
			content, err := io.ReadAll(reader)
			require.NoError(t, err, "Failed to read content: %v", err)

			var count int
			decoder := json.NewDecoder(bytes.NewReader(content))
			for decoder.More() {
				var entry map[string]interface{}
				err := decoder.Decode(&entry)
				require.NoError(t, err, "Failed to decode result: %v", err)
				count++

				if tt.validate != nil {
					tt.validate(t, entry)
				}
			}

			require.Equal(t, tt.wantCount, count, "Got %d entries, want %d", count, tt.wantCount)
		})
	}
}

// createBlob is a helper function that creates a JSON blob with a trailing newline
func createBlob(t *testing.T, data map[string]interface{}) []byte {
	blob, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Failed to create blob: %v", err)
	}
	return append(blob, '\n')
}
