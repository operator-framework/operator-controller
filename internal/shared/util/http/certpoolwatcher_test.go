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

func TestCertPoolWatcher(t *testing.T) {
	// create a temporary directory
	tmpDir, err := os.MkdirTemp("", "cert-pool")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// create the first cert
	certName := filepath.Join(tmpDir, "test1.pem")
	t.Logf("Create cert file at %q\n", certName)
	createCert(t, certName)

	// Update environment variables for the watcher - some of these should not exist
	os.Setenv("SSL_CERT_DIR", tmpDir+":/tmp/does-not-exist.dir")
	os.Setenv("SSL_CERT_FILE", "/tmp/does-not-exist.file")

	// Create the cert pool watcher
	cpw, err := httputil.NewCertPoolWatcher(tmpDir, log.FromContext(context.Background()))
	require.NoError(t, err)
	defer cpw.Done()

	// Get the original pool
	firstPool, firstGen, err := cpw.Get()
	require.NoError(t, err)
	require.NotNil(t, firstPool)

	// Create a second cert
	certName = filepath.Join(tmpDir, "test2.pem")
	t.Logf("Create cert file at %q\n", certName)
	createCert(t, certName)

	require.Eventually(t, func() bool {
		secondPool, secondGen, err := cpw.Get()
		if err != nil {
			return false
		}
		return secondGen != firstGen && !firstPool.Equal(secondPool)
	}, 30*time.Second, time.Second)
}
