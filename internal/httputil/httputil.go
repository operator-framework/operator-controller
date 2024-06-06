package httputil

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"time"
)

func BuildHTTPClient(caCert string) (*http.Client, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	if caCert != "" {
		// tlsFileWatcher, err := certwatcher.New(caCert, "")

		cert, err := os.ReadFile(caCert)
		if err != nil {
			return nil, err
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(cert)
		tlsConfig := &tls.Config{
			RootCAs:    caCertPool,
			MinVersion: tls.VersionTLS12,
		}
		tlsTransport := &http.Transport{
			TLSClientConfig: tlsConfig,
		}
		httpClient.Transport = tlsTransport
	}

	return httpClient, nil
}
