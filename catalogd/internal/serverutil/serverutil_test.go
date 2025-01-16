package serverutil

import (
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestStorageServerHandlerWrapped_Gzip(t *testing.T) {
	tests := []struct {
		name             string
		acceptEncoding   string
		responseContent  string
		expectCompressed bool
		expectedStatus   int
	}{
		{
			name:             "compresses large response when client accepts gzip",
			acceptEncoding:   "gzip",
			responseContent:  testCompressableJSON,
			expectCompressed: true,
			expectedStatus:   http.StatusOK,
		},
		{
			name:             "does not compress small response even when client accepts gzip",
			acceptEncoding:   "gzip",
			responseContent:  `{"foo":"bar"}`,
			expectCompressed: false,
			expectedStatus:   http.StatusOK,
		},
		{
			name:             "does not compress when client doesn't accept gzip",
			acceptEncoding:   "",
			responseContent:  testCompressableJSON,
			expectCompressed: false,
			expectedStatus:   http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock storage instance that returns our test content
			mockStorage := &mockStorageInstance{
				content: tt.responseContent,
			}
			mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{})
			require.NoError(t, err)

			cfg := CatalogServerConfig{
				LocalStorage: mockStorage,
			}
			handler := storageServerHandlerWrapped(mgr, cfg)

			// Create test request
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tt.acceptEncoding)
			}

			// Create response recorder
			rec := httptest.NewRecorder()

			// Handle the request
			handler.ServeHTTP(rec, req)

			// Check status code
			require.Equal(t, tt.expectedStatus, rec.Code)

			// Check if response was compressed
			wasCompressed := rec.Header().Get("Content-Encoding") == "gzip"
			require.Equal(t, tt.expectCompressed, wasCompressed)

			// Get the response body
			var responseBody []byte
			if wasCompressed {
				// Decompress the response
				gzipReader, err := gzip.NewReader(rec.Body)
				require.NoError(t, err)
				responseBody, err = io.ReadAll(gzipReader)
				require.NoError(t, err)
				require.NoError(t, gzipReader.Close())
			} else {
				responseBody = rec.Body.Bytes()
			}

			// Verify the response content
			require.Equal(t, tt.responseContent, string(responseBody))
		})
	}
}

const testCompressableJSON = `{
	"defaultChannel": "stable-v6.x",
	"name": "cockroachdb",
	"schema": "olm.package"
  }
  {
	"entries": [
	  {
		"name": "cockroachdb.v5.0.3"
	  },
	  {
		"name": "cockroachdb.v5.0.4",
		"replaces": "cockroachdb.v5.0.3"
	  }
	],
	"name": "stable-5.x",
	"package": "cockroachdb",
	"schema": "olm.channel"
  }
  {
	"entries": [
	  {
		"name": "cockroachdb.v6.0.0",
		"skipRange": "<6.0.0"
	  }
	],
	"name": "stable-v6.x",
	"package": "cockroachdb",
	"schema": "olm.channel"
  }
  {
	"image": "quay.io/openshift-community-operators/cockroachdb@sha256:a5d4f4467250074216eb1ba1c36e06a3ab797d81c431427fc2aca97ecaf4e9d8",
	"name": "cockroachdb.v5.0.3",
	"package": "cockroachdb",
	"properties": [
	  {
		"type": "olm.gvk",
		"value": {
		  "group": "charts.operatorhub.io",
		  "kind": "Cockroachdb",
		  "version": "v1alpha1"
		}
	  },
	  {
		"type": "olm.package",
		"value": {
		  "packageName": "cockroachdb",
		  "version": "5.0.3"
		}
	  }
	],
	"relatedImages": [
	  {
		"name": "",
		"image": "quay.io/helmoperators/cockroachdb:v5.0.3"
	  },
	  {
		"name": "",
		"image": "quay.io/openshift-community-operators/cockroachdb@sha256:a5d4f4467250074216eb1ba1c36e06a3ab797d81c431427fc2aca97ecaf4e9d8"
	  }
	],
	"schema": "olm.bundle"
  }
  {
	"image": "quay.io/openshift-community-operators/cockroachdb@sha256:f42337e7b85a46d83c94694638e2312e10ca16a03542399a65ba783c94a32b63",
	"name": "cockroachdb.v5.0.4",
	"package": "cockroachdb",
	"properties": [
	  {
		"type": "olm.gvk",
		"value": {
		  "group": "charts.operatorhub.io",
		  "kind": "Cockroachdb",
		  "version": "v1alpha1"
		}
	  },
	  {
		"type": "olm.package",
		"value": {
		  "packageName": "cockroachdb",
		  "version": "5.0.4"
		}
	  }
	],
	"relatedImages": [
	  {
		"name": "",
		"image": "quay.io/helmoperators/cockroachdb:v5.0.4"
	  },
	  {
		"name": "",
		"image": "quay.io/openshift-community-operators/cockroachdb@sha256:f42337e7b85a46d83c94694638e2312e10ca16a03542399a65ba783c94a32b63"
	  }
	],
	"schema": "olm.bundle"
  }
  {
	"image": "quay.io/openshift-community-operators/cockroachdb@sha256:d3016b1507515fc7712f9c47fd9082baf9ccb070aaab58ed0ef6e5abdedde8ba",
	"name": "cockroachdb.v6.0.0",
	"package": "cockroachdb",
	"properties": [
	  {
		"type": "olm.gvk",
		"value": {
		  "group": "charts.operatorhub.io",
		  "kind": "Cockroachdb",
		  "version": "v1alpha1"
		}
	  },
	  {
		"type": "olm.package",
		"value": {
		  "packageName": "cockroachdb",
		  "version": "6.0.0"
		}
	  }
	],
	"relatedImages": [
	  {
		"name": "",
		"image": "quay.io/cockroachdb/cockroach-helm-operator:6.0.0"
	  },
	  {
		"name": "",
		"image": "quay.io/openshift-community-operators/cockroachdb@sha256:d3016b1507515fc7712f9c47fd9082baf9ccb070aaab58ed0ef6e5abdedde8ba"
	  }
	],
	"schema": "olm.bundle"
  }
  `

// mockStorageInstance implements storage.Instance interface for testing
type mockStorageInstance struct {
	content string
}

func (m *mockStorageInstance) StorageServerHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(m.content))
	})
}

func (m *mockStorageInstance) Store(ctx context.Context, catalogName string, fs fs.FS) error {
	return nil
}

func (m *mockStorageInstance) Delete(catalogName string) error {
	return nil
}

func (m *mockStorageInstance) ContentExists(catalog string) bool {
	return true
}
func (m *mockStorageInstance) BaseURL(catalog string) string {
	return ""
}
