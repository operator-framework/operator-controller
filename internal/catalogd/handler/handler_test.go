package handler_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/catalogd/handler"
)

func TestStandardHandler_Gzip(t *testing.T) {
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
			baseHandler := handler.NewSubPathHandler("/test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tt.responseContent))
			}))

			standardHandler := handler.NewStandardHandler(baseHandler)

			// Create test request
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tt.acceptEncoding)
			}

			// Create response recorder
			rec := httptest.NewRecorder()

			// Handle the request
			standardHandler.ServeHTTP(rec, req)

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
