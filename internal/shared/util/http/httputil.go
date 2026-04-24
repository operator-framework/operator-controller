package http

import (
	"crypto/tls"
	"net/http"
	"time"
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
	httpClient.Transport = &http.Transport{
		TLSClientConfig: tlsConfig,
		// Proxy must be set explicitly; a nil Proxy field means "no proxy" and
		// ignores HTTPS_PROXY/NO_PROXY env vars.  Only http.DefaultTransport sets
		// this by default; custom transports must opt in.
		Proxy: http.ProxyFromEnvironment,
	}

	return httpClient, nil
}
