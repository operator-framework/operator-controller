package httputil

import (
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	firstExpiration := time.Time{}

	for _, e := range dirEntries {
		file := filepath.Join(caDir, e.Name())
		// These might be symlinks pointing to directories, so use Stat() to resolve
		fi, err := os.Stat(file)
		if err != nil {
			return nil, err
		}
		if fi.IsDir() {
			log.Info("skip directory", "name", e.Name())
			continue
		}
		log.Info("load certificate", "name", e.Name())
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("error reading cert file %q: %w", file, err)
		}
		// The return indicates if any certs were added
		if caCertPool.AppendCertsFromPEM(data) {
			count++
		}
	}

	// Found no certs!
	if count == 0 {
		return nil, fmt.Errorf("no certificates found in %q", caDir)
	}

	log.Info("first expiration", "time", firstExpiration.Format(time.RFC3339))
	return caCertPool, nil
}
