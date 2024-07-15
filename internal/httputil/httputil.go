package httputil

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// This version of (*x509.CertPool).AppendCertsFromPEM() will error out if parsing fails
func appendCertsFromPEM(s *x509.CertPool, pemCerts []byte) error {
	n := 1
	for len(pemCerts) > 0 {
		var block *pem.Block
		block, pemCerts = pem.Decode(pemCerts)
		if block == nil {
			return fmt.Errorf("unable to PEM decode cert %d", n)
		}
		// ignore non-certificates (e.g. keys)
		if block.Type != "CERTIFICATE" {
			continue
		}
		if len(block.Headers) != 0 {
			// This is a cert, but we're ignoring it, so bump the counter
			n++
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("unable to parse cert %d: %w", n, err)
		}
		// no return values - panics or always succeeds
		s.AddCert(cert)
		n++
	}

	return nil
}

func NewCertPool(caDir string) (*x509.CertPool, error) {
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	if caDir == "" {
		return caCertPool, nil
	}

	dirEntries, err := os.ReadDir(caDir)
	if err != nil {
		return nil, err
	}
	for _, e := range dirEntries {
		if e.IsDir() {
			continue
		}
		file := filepath.Join(caDir, e.Name())
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("error reading cert file %q: %w", file, err)
		}
		err = appendCertsFromPEM(caCertPool, data)
		if err != nil {
			return nil, fmt.Errorf("error adding cert file %q: %w", file, err)
		}
	}

	return caCertPool, nil
}

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
