package http

import (
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
)

func NewCertPool(caDir string, log logr.Logger) (*x509.CertPool, error) {
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
	count := 0

	for _, e := range dirEntries {
		file := filepath.Join(caDir, e.Name())
		// These might be symlinks pointing to directories, so use Stat() to resolve
		fi, err := os.Stat(file)
		if err != nil {
			return nil, err
		}
		if fi.IsDir() {
			log.V(defaultLogLevel).Info("skip directory", "name", e.Name())
			continue
		}
		log.V(defaultLogLevel).Info("reading certificate file", "name", e.Name(), "size", fi.Size(), "modtime", fi.ModTime())
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("error reading cert file %q: %w", file, err)
		}
		// The return indicates if any certs were added
		if caCertPool.AppendCertsFromPEM(data) {
			count++
		}
		logPem(data, e.Name(), caDir, "loading certificate", log)
	}

	// Found no certs!
	if count == 0 {
		return nil, fmt.Errorf("no certificates found in %q", caDir)
	}

	return caCertPool, nil
}
