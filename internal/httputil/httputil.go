package httputil

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func LoadCerts(caDir string) (string, error) {
	if caDir == "" {
		return "", nil
	}

	certs := []string{}
	dirEntries, err := os.ReadDir(caDir)
	if err != nil {
		return "", err
	}
	for _, e := range dirEntries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(caDir, e.Name()))
		if err != nil {
			return "", err
		}
		certs = append(certs, string(data))
	}
	return strings.Join(certs, "\n"), nil
}

func BuildHTTPClient(caDir string) (*http.Client, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// use the SystemCertPool as a default
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}

	certs, err := LoadCerts(caDir)
	if err != nil {
		return nil, err
	}

	caCertPool.AppendCertsFromPEM([]byte(certs))
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
