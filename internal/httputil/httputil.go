package httputil

import (
	"crypto/tls"
	"net/http"
	"time"
)

func BuildHTTPClient(cpw *CertPoolWatcher) (*http.Client, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	pool, err := cpw.Get()
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}
	tlsTransport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	httpClient.Transport = tlsTransport

	return httpClient, nil
}
