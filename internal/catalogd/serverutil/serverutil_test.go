package serverutil

import (
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

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

// writeTempCert generates a self-signed TLS certificate and writes the PEM-encoded
// cert and key to temporary files. The files are cleaned up when the test ends.
func writeTempCert(t *testing.T) (string, string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	privDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})

	cf, err := os.CreateTemp(t.TempDir(), "cert*.pem")
	require.NoError(t, err)
	_, err = cf.Write(certPEM)
	require.NoError(t, err)
	require.NoError(t, cf.Close())

	kf, err := os.CreateTemp(t.TempDir(), "key*.pem")
	require.NoError(t, err)
	_, err = kf.Write(keyPEM)
	require.NoError(t, err)
	require.NoError(t, kf.Close())

	return cf.Name(), kf.Name()
}

// TestCatalogServerTLSOptsApplied verifies that TLSOpts provided in CatalogServerConfig
// are applied to the TLS configuration when the catalog server starts.
func TestCatalogServerTLSOptsApplied(t *testing.T) {
	certFile, keyFile := writeTempCert(t)

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	// Track whether our TLSOpt was called and record the resulting MinVersion.
	// Configure a certificate source and MinVersion, mirroring the real tlsOpts+tlsProfile pattern.
	var observedMinVersion uint16
	tlsOpt := func(c *tls.Config) {
		c.Certificates = []tls.Certificate{cert}
		c.MinVersion = tls.VersionTLS13
		observedMinVersion = c.MinVersion
	}

	mockStorage := &mockStorageInstance{}
	cfg := CatalogServerConfig{
		CatalogAddr:  "127.0.0.1:0",
		CertFile:     certFile,
		KeyFile:      keyFile,
		LocalStorage: mockStorage,
		TLSOpts:      []func(*tls.Config){tlsOpt},
	}
	r := &catalogServerRunnable{
		cfg: cfg,
		server: &http.Server{
			Handler:      storageServerHandlerWrapped(logr.Logger{}, cfg),
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Minute,
		},
		shutdownTimeout: time.Second,
		ready:           make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- r.Start(ctx)
	}()

	select {
	case <-r.ready:
	case err := <-errCh:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start in time")
	}

	require.EqualValues(t, tls.VersionTLS13, observedMinVersion, "TLSOpts must be applied to the TLS config")

	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop in time")
	}
}

// TestCatalogServerTLSOptsCertSourceRequired verifies that the catalog server
// returns an error if TLSOpts do not configure any certificate source.
func TestCatalogServerTLSOptsCertSourceRequired(t *testing.T) {
	certFile, keyFile := writeTempCert(t)

	mockStorage := &mockStorageInstance{}
	cfg := CatalogServerConfig{
		CatalogAddr:  "127.0.0.1:0",
		CertFile:     certFile,
		KeyFile:      keyFile,
		LocalStorage: mockStorage,
		TLSOpts:      []func(*tls.Config){},
	}
	r := &catalogServerRunnable{
		cfg: cfg,
		server: &http.Server{
			Handler:      storageServerHandlerWrapped(logr.Logger{}, cfg),
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Minute,
		},
		shutdownTimeout: time.Second,
		ready:           make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := r.Start(ctx)
	require.ErrorContains(t, err, "TLSOpts must configure a certificate source")
}
