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

	var certs []string
	err := filepath.Walk(caDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return filepath.SkipDir
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		certs = append(certs, string(data))
		return nil
	})
	if err != nil {
		return "", err
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
