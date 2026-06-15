package http

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/operator-framework/operator-controller/internal/shared/util/tlsprofiles"
)

func BuildHTTPClient(cpw *CertPoolWatcher) (*http.Client, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	pool, _, err := cpw.Get()
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}
	tlsProfile, err := tlsprofiles.GetTLSConfigFunc()
	if err != nil {
		return nil, fmt.Errorf("getting TLS config func: %w", err)
	}
	tlsProfile(tlsConfig)
	httpClient.Transport = &http.Transport{
		TLSClientConfig: tlsConfig,
		// Proxy must be set explicitly; a nil Proxy field means "no proxy" and
		// ignores HTTPS_PROXY/NO_PROXY env vars.  Only http.DefaultTransport sets
		// this by default; custom transports must opt in.
		Proxy: http.ProxyFromEnvironment,
	}

	return httpClient, nil
}
