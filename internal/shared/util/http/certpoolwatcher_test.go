package http_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"

	httputil "github.com/operator-framework/operator-controller/internal/shared/util/http"
)

func createCert(t *testing.T, name string) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	notBefore := time.Now()
	notAfter := notBefore.Add(time.Hour)
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{name},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		IsCA: true,

		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},

		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)

	certOut, err := os.Create(name)
	require.NoError(t, err)

	err = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	require.NoError(t, err)

	err = certOut.Close()
	require.NoError(t, err)

	// ignore the key
}

func createTempCaDir(t *testing.T) string {
	tmpCaDir, err := os.MkdirTemp("", "ca-dir")
	require.NoError(t, err)
	createCert(t, filepath.Join(tmpCaDir, "test1.pem"))
	return tmpCaDir
}

func TestCertPoolWatcherCaDir(t *testing.T) {
	// create a temporary CA directory
	tmpCaDir := createTempCaDir(t)
	defer os.RemoveAll(tmpCaDir)

	os.Unsetenv("SSL_CERT_FILE")
	os.Unsetenv("SSL_CERT_DIR")

	// Create the cert pool watcher
	cpw, err := httputil.NewCertPoolWatcher(tmpCaDir, log.FromContext(context.Background()))
	require.NoError(t, err)
	require.NotNil(t, cpw)
	defer cpw.Done()
	restarted := &atomic.Bool{}
	restarted.Store(false)
	cpw.Restart(func(int) { restarted.Store(true) })
	err = cpw.Start(context.Background())
	require.NoError(t, err)

	// Get the original pool
	firstPool, firstGen, err := cpw.Get()
	require.NoError(t, err)
	require.NotNil(t, firstPool)

	// Create a second cert in the CA directory
	certName := filepath.Join(tmpCaDir, "test2.pem")
	t.Logf("Create cert file at %q\n", certName)
	createCert(t, certName)

	require.Eventually(t, func() bool {
		secondPool, secondGen, err := cpw.Get()
		if err != nil {
			return false
		}
		// Should NOT restart, because this is not SSL_CERT_DIR nor SSL_CERT_FILE
		return secondGen != firstGen && !firstPool.Equal(secondPool) && !restarted.Load()
	}, 10*time.Second, time.Second)
}

func TestCertPoolWatcherSslCertDir(t *testing.T) {
	// create a temporary CA directory for SSL_CERT_DIR
	tmpSslDir := createTempCaDir(t)
	defer os.RemoveAll(tmpSslDir)

	// Update environment variables for the watcher - some of these should not exist
	os.Unsetenv("SSL_CERT_FILE")
	os.Setenv("SSL_CERT_DIR", tmpSslDir+":/tmp/does-not-exist.dir")
	defer os.Unsetenv("SSL_CERT_DIR")

	// Create a different CaDir
	tmpCaDir := createTempCaDir(t)
	defer os.RemoveAll(tmpCaDir)

	// Create the cert pool watcher
	cpw, err := httputil.NewCertPoolWatcher(tmpCaDir, log.FromContext(context.Background()))
	require.NoError(t, err)
	restarted := &atomic.Bool{}
	restarted.Store(false)
	cpw.Restart(func(int) { restarted.Store(true) })
	err = cpw.Start(context.Background())
	require.NoError(t, err)
	defer cpw.Done()

	// Get the original pool
	firstPool, firstGen, err := cpw.Get()
	require.NoError(t, err)
	require.NotNil(t, firstPool)

	// Create a second cert in SSL_CIR_DIR
	certName := filepath.Join(tmpSslDir, "test2.pem")
	t.Logf("Create cert file at %q\n", certName)
	createCert(t, certName)

	require.Eventually(t, func() bool {
		_, secondGen, err := cpw.Get()
		if err != nil {
			return false
		}
		// Because SSL_CERT_DIR is part of the SystemCertPool:
		// 1. CPW only watches: it doesn't actually load it, that's the SystemCertPool's responsibility
		// 2. Because the SystemCertPool never changes, we can't directly compare the pools
		// 3. If SSL_CERT_DIR changes, we should expect a restart
		return secondGen != firstGen && restarted.Load()
	}, 10*time.Second, time.Second)
}

func TestCertPoolWatcherSslCertFile(t *testing.T) {
	// create a temporary directory
	tmpDir, err := os.MkdirTemp("", "cert-pool")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// create the first cert
	certName := filepath.Join(tmpDir, "test1.pem")
	t.Logf("Create cert file at %q\n", certName)
	createCert(t, certName)

	// Update environment variables for the watcher
	os.Unsetenv("SSL_CERT_DIR")
	os.Setenv("SSL_CERT_FILE", certName)
	defer os.Unsetenv("SSL_CERT_FILE")

	// Create a different CaDir
	tmpCaDir := createTempCaDir(t)
	defer os.RemoveAll(tmpCaDir)

	// Create the cert pool watcher
	cpw, err := httputil.NewCertPoolWatcher(tmpCaDir, log.FromContext(context.Background()))
	require.NoError(t, err)
	require.NotNil(t, cpw)
	defer cpw.Done()
	restarted := &atomic.Bool{}
	restarted.Store(false)
	cpw.Restart(func(int) { restarted.Store(true) })
	err = cpw.Start(context.Background())
	require.NoError(t, err)

	// Get the original pool
	firstPool, firstGen, err := cpw.Get()
	require.NoError(t, err)
	require.NotNil(t, firstPool)

	// Update the SSL_CERT_FILE
	t.Logf("Create cert file at %q\n", certName)
	createCert(t, certName)

	require.Eventually(t, func() bool {
		_, secondGen, err := cpw.Get()
		if err != nil {
			return false
		}
		// Because SSL_CERT_FILE is part of the SystemCertPool:
		// 1. CPW only watches: it doesn't actually load it, that's the SystemCertPool's responsibility
		// 2. Because the SystemCertPool never changes, we can't directly compare the pools
		// 3. If SSL_CERT_FILE changes, we should expect a restart
		return secondGen != firstGen && restarted.Load()
	}, 10*time.Second, time.Second)
}

func TestCertPoolWatcherEmpty(t *testing.T) {
	os.Unsetenv("SSL_CERT_FILE")
	os.Unsetenv("SSL_CERT_DIR")

	// Create the empty cert pool watcher
	cpw, err := httputil.NewCertPoolWatcher("", log.FromContext(context.Background()))
	require.NoError(t, err)
	require.NotNil(t, cpw)
	defer cpw.Done()
	err = cpw.Start(context.Background())
	require.NoError(t, err)

	pool, _, err := cpw.Get()
	require.NoError(t, err)
	require.NotNil(t, pool)
}

func TestCertPoolInvalidPath(t *testing.T) {
	os.Unsetenv("SSL_CERT_FILE")
	os.Unsetenv("SSL_CERT_DIR")

	// Create an invalid cert pool watcher
	cpw, err := httputil.NewCertPoolWatcher("/this/path/should/not/exist", log.FromContext(context.Background()))
	require.NoError(t, err)
	require.NotNil(t, cpw)
	defer cpw.Done()
	err = cpw.Start(context.Background())
	require.Error(t, err)

	pool, _, err := cpw.Get()
	require.Error(t, err)
	require.Nil(t, pool)
}
