package httputil

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"time"
)

func BuildHTTPClient(caCertPool *x509.CertPool) (*http.Client, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	tlsConfig := &tls.Config{
		RootCAs:    caCertPool,
		MinVersion: tls.VersionTLS12,
	}
	tlsTransport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	httpClient.Transport = tlsTransport

	return httpClient, nil
}
