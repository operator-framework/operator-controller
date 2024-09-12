package httputil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
)

func TestNewCertPool(t *testing.T) {
	t.Parallel()

	// set up our CA certificate
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(2019),
		Subject: pkix.Name{
			Organization:  []string{"Company, INC."},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{"Golden Gate Bridge"},
			PostalCode:    []string{"94016"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// create our private and public key
	caPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	require.NoError(t, err)

	// create the CA
	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrivKey.PublicKey, caPrivKey)
	require.NoError(t, err)

	// pem encode
	err = os.MkdirAll("testdata/newCertPool/subfolder", 0700)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll("testdata/newCertPool"))
	})

	caPEM, err := os.Create("testdata/newCertPool/my.pem")
	require.NoError(t, err)
	err = pem.Encode(caPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})
	require.NoError(t, err)

	_, err = NewCertPool("testdata/newCertPool", testr.New(t))
	require.NoError(t, err)
}

func Test_newCertPool_empty(t *testing.T) {
	err := os.MkdirAll("testdata/newCertPoolEmpty", 0700)
	require.NoError(t, err)

	_, err = NewCertPool("testdata/newCertPoolEmpty", testr.New(t))
	require.EqualError(t, err, `no certificates found in "testdata/newCertPoolEmpty"`)
}
