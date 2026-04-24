package http_test

import (
	"context"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"

	httputil "github.com/operator-framework/operator-controller/internal/shared/util/http"
)

// startRecordingProxy starts a plain-HTTP CONNECT proxy that tunnels HTTPS
// connections and records the target host of each CONNECT request.
func startRecordingProxy(proxied chan<- string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "only CONNECT supported", http.StatusMethodNotAllowed)
			return
		}
		// Non-blocking: if there are unexpected extra CONNECT requests (retries,
		// parallel connections) we record the first one and drop the rest rather
		// than blocking the proxy handler goroutine.
		select {
		case proxied <- r.Host:
		default:
		}

		dst, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer dst.Close()

		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking not supported", http.StatusInternalServerError)
			return
		}
		conn, bufrw, err := hj.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer conn.Close()

		if _, err = conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
			return
		}

		done := make(chan struct{}, 2)
		tunnel := func(dst io.Writer, src io.Reader) {
			defer func() { done <- struct{}{} }()
			_, _ = io.Copy(dst, src)
			// Half-close the write side so the other direction sees EOF and
			// its io.Copy returns, preventing the goroutine from hanging.
			if cw, ok := dst.(interface{ CloseWrite() error }); ok {
				_ = cw.CloseWrite()
			}
		}
		// Use bufrw (not conn) as the client→dst source: Hijack may have
		// buffered bytes (e.g. the TLS ClientHello) that arrived together with
		// the CONNECT headers; reading from conn directly would lose them.
		go tunnel(dst, bufrw)
		go tunnel(conn, dst)
		<-done
		<-done // wait for both directions before closing connections
	}))
}

// certPoolWatcherForTLSServer creates a CertPoolWatcher that trusts the given
// TLS test server's certificate.
func certPoolWatcherForTLSServer(t *testing.T, server *httptest.Server) *httputil.CertPoolWatcher {
	t.Helper()

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.pem")

	certDER := server.TLS.Certificates[0].Certificate[0]
	f, err := os.Create(certPath)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	require.NoError(t, f.Close())

	cpw, err := httputil.NewCertPoolWatcher(dir, log.FromContext(context.Background()))
	require.NoError(t, err)
	require.NotNil(t, cpw)
	t.Cleanup(cpw.Done)
	require.NoError(t, cpw.Start(context.Background()))
	return cpw
}

// TestBuildHTTPClientTransportUsesProxyFromEnvironment verifies that the
// transport returned by BuildHTTPClient has Proxy set to http.ProxyFromEnvironment
// so that HTTPS_PROXY and NO_PROXY env vars are honoured at runtime.
func TestBuildHTTPClientTransportUsesProxyFromEnvironment(t *testing.T) {
	// Use system certs (empty dir) — we only need a valid CertPoolWatcher.
	cpw, err := httputil.NewCertPoolWatcher("", log.FromContext(context.Background()))
	require.NoError(t, err)
	t.Cleanup(cpw.Done)
	require.NoError(t, cpw.Start(context.Background()))

	client, err := httputil.BuildHTTPClient(cpw)
	require.NoError(t, err)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.Equal(t,
		reflect.ValueOf(http.ProxyFromEnvironment).Pointer(),
		reflect.ValueOf(transport.Proxy).Pointer(),
		"BuildHTTPClient must wire transport.Proxy to http.ProxyFromEnvironment so that "+
			"HTTPS_PROXY/NO_PROXY env vars are honoured; a nil or different Proxy function "+
			"means env-var proxying is silently disabled")
}

// TestBuildHTTPClientProxyTunnelsConnections verifies end-to-end that the
// HTTP client produced by BuildHTTPClient correctly tunnels HTTPS connections
// through an HTTP CONNECT proxy.
//
// The test overrides transport.Proxy with http.ProxyURL rather than relying on
// HTTPS_PROXY: httptest servers bind to 127.0.0.1, which http.ProxyFromEnvironment
// silently excludes from proxying, and env-var changes within the same process
// are unreliable due to sync.Once caching.  Using http.ProxyURL directly exercises
// the same tunnelling code path that HTTPS_PROXY triggers in production.
func TestBuildHTTPClientProxyTunnelsConnections(t *testing.T) {
	targetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	proxied := make(chan string, 1)
	proxyServer := startRecordingProxy(proxied)
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)

	cpw := certPoolWatcherForTLSServer(t, targetServer)
	client, err := httputil.BuildHTTPClient(cpw)
	require.NoError(t, err)

	// Point the transport directly at our test proxy, bypassing the loopback
	// exclusion and env-var caching of http.ProxyFromEnvironment.
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	transport.Proxy = http.ProxyURL(proxyURL)

	resp, err := client.Get(targetServer.URL)
	require.NoError(t, err)
	resp.Body.Close()

	select {
	case host := <-proxied:
		require.Equal(t, targetServer.Listener.Addr().String(), host,
			"proxy must have received a CONNECT request for the target server address")
	case <-time.After(5 * time.Second):
		t.Fatal("HTTPS connection to target server did not go through the proxy")
	}
}

// TestBuildHTTPClientProxyBlocksWhenRejected verifies that when the proxy
// rejects the CONNECT tunnel, the client request fails rather than silently
// falling back to a direct connection.
func TestBuildHTTPClientProxyBlocksWhenRejected(t *testing.T) {
	targetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	// A proxy that returns 403 Forbidden for every CONNECT request.
	rejectingProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			http.Error(w, "proxy access denied", http.StatusForbidden)
			return
		}
		http.Error(w, "only CONNECT supported", http.StatusMethodNotAllowed)
	}))
	defer rejectingProxy.Close()

	proxyURL, err := url.Parse(rejectingProxy.URL)
	require.NoError(t, err)

	cpw := certPoolWatcherForTLSServer(t, targetServer)
	client, err := httputil.BuildHTTPClient(cpw)
	require.NoError(t, err)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	transport.Proxy = http.ProxyURL(proxyURL)

	resp, err := client.Get(targetServer.URL)
	if resp != nil {
		resp.Body.Close()
	}
	require.Error(t, err, "request should fail when the proxy rejects the CONNECT tunnel")
}
