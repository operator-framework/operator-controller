package http

import (
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
)

func readCertFile(pool *x509.CertPool, file string, log logr.Logger) (bool, error) {
	certRead := false
	if file == "" {
		return certRead, nil
	}
	// These might be symlinks pointing to directories, so use Stat() to resolve
	fi, err := os.Stat(file)
	if err != nil {
		// Ignore files that don't exist
		if os.IsNotExist(err) {
			return certRead, nil
		}
		return certRead, err
	}
	if fi.IsDir() {
		log.V(defaultLogLevel).Info("skip directory", "name", file)
		return certRead, nil
	}
	log.V(defaultLogLevel).Info("load certificate", "name", file, "size", fi.Size(), "modtime", fi.ModTime())
	data, err := os.ReadFile(file)
	if err != nil {
		return certRead, fmt.Errorf("error reading cert file %q: %w", file, err)
	}
	// The return indicates if any certs were added
	if pool.AppendCertsFromPEM(data) {
		certRead = true
	}
	logPem(data, filepath.Base(file), filepath.Dir(file), "loading certificate file", log)

	return certRead, nil
}

func readCertDir(pool *x509.CertPool, dir string, log logr.Logger) (bool, error) {
	certRead := false
	if dir == "" {
		return certRead, nil
	}
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		// Ignore directories that don't exist
		if os.IsNotExist(err) {
			return certRead, nil
		}
		return certRead, err
	}

	for _, e := range dirEntries {
		file := filepath.Join(dir, e.Name())
		c, err := readCertFile(pool, file, log)
		if err != nil {
			return certRead, err
		}
		certRead = certRead || c
	}
	return certRead, nil
}

// This function looks explicitly at the SSL environment, and
// uses it to create a "fresh" system cert pool
func systemCertPool(log logr.Logger) (*x509.CertPool, error) {
	sslCertDir := os.Getenv("SSL_CERT_DIR")
	sslCertFile := os.Getenv("SSL_CERT_FILE")
	if sslCertDir == "" && sslCertFile == "" {
		log.V(defaultLogLevel).Info("SystemCertPool: SSL environment not set")
		return x509.SystemCertPool()
	}
	log.V(defaultLogLevel).Info("SystemCertPool: SSL environment set", "SSL_CERT_DIR", sslCertDir, "SSL_CERT_FILE", sslCertFile)

	certRead := false
	pool := x509.NewCertPool()

	// SSL_CERT_DIR may consist of multiple entries separated by ":"
	for _, d := range strings.Split(sslCertDir, ":") {
		c, err := readCertDir(pool, d, log)
		if err != nil {
			return nil, err
		}
		certRead = certRead || c
	}
	// SSL_CERT_FILE may consist of only a single entry
	c, err := readCertFile(pool, sslCertFile, log)
	if err != nil {
		return nil, err
	}
	certRead = certRead || c

	// If SSL_CERT_DIR and SSL_CERT_FILE resulted in no certs, then return the system cert pool
	if !certRead {
		return x509.SystemCertPool()
	}
	return pool, nil
}

func NewCertPool(caDir string, log logr.Logger) (*x509.CertPool, error) {
	caCertPool, err := systemCertPool(log)
	if err != nil {
		return nil, err
	}

	if caDir == "" {
		return caCertPool, nil
	}
	readCert, err := readCertDir(caCertPool, caDir, log)
	if err != nil {
		return nil, err
	}

	// Found no certs!
	if !readCert {
		return nil, fmt.Errorf("no certificates found in %q", caDir)
	}

	return caCertPool, nil
}
