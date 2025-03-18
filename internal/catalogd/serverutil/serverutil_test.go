package serverutil

import (
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
)

func TestStorageServerHandlerWrapped_Gzip(t *testing.T) {
	var generatedJSON = func(size int) string {
		return "{\"data\":\"" + strings.Repeat("test data ", size) + "\"}"
	}
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
			responseContent:  generatedJSON(1000),
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
			responseContent:  generatedJSON(1000),
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

			cfg := CatalogServerConfig{
				LocalStorage: mockStorage,
			}
			handler := storageServerHandlerWrapped(logr.Logger{}, cfg)

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

// mockStorageInstance implements storage.Instance interface for testing
type mockStorageInstance struct {
	content string
}

func (m *mockStorageInstance) StorageServerHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(m.content))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

func (m *mockStorageInstance) ForgetIndex(catalogName string) {}
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
