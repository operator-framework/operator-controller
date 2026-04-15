package tlsprofiles

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// generateSelfSignedCert generates a self-signed ECDSA P-256 certificate for use in tests.
func generateSelfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	privDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	return cert
}

// startTLSServer starts a TLS listener with cfgFn applied and serves connections in
// the background. The listener is closed when the test completes.
func startTLSServer(t *testing.T, cfgFn func(*tls.Config)) string {
	t.Helper()

	serverCfg := &tls.Config{
		Certificates: []tls.Certificate{generateSelfSignedCert(t)},
		MinVersion:   tls.VersionTLS12, // baseline; cfgFn will raise this if the profile requires it
	}
	cfgFn(serverCfg)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverCfg)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go func() {
				defer conn.Close()
				_ = conn.(*tls.Conn).Handshake()
			}()
		}
	}()

	return ln.Addr().String()
}

// dialTLS connects to addr with the given config and returns the negotiated
// ConnectionState. The caller must check err before using the state.
func dialTLS(addr string, clientCfg *tls.Config) (tls.ConnectionState, error) {
	conn, err := tls.Dial("tcp", addr, clientCfg)
	if err != nil {
		return tls.ConnectionState{}, err
	}
	defer conn.Close()
	return conn.ConnectionState(), nil
}

// setCustomProfile configures the package-level custom TLS profile for the duration
// of the test and restores the original state via t.Cleanup.
func setCustomProfile(t *testing.T, cipherNames []string, curveNames []string, minVersion string) {
	t.Helper()

	origProfile := configuredProfile
	origCustom := customTLSProfile
	t.Cleanup(func() {
		configuredProfile = origProfile
		customTLSProfile = origCustom
	})

	configuredProfile = "custom"
	customTLSProfile = tlsProfile{
		ciphers: cipherSlice{},
		curves:  curveSlice{},
	}

	for _, name := range cipherNames {
		require.NoError(t, customTLSProfile.ciphers.Append(name))
	}
	for _, name := range curveNames {
		require.NoError(t, customTLSProfile.curves.Append(name))
	}
	if minVersion != "" {
		require.NoError(t, customTLSProfile.minTLSVersion.Set(minVersion))
	}
}

// TestCustomTLSProfileCipherNegotiation verifies that when a custom profile
// specifies a single cipher suite, that cipher is actually negotiated.
func TestCustomTLSProfileCipherNegotiation(t *testing.T) {
	const cipher = "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"
	cipherID := cipherSuiteId(cipher)
	require.NotZero(t, cipherID)

	setCustomProfile(t, []string{cipher}, []string{"prime256v1"}, "TLSv1.2")

	cfgFn, err := GetTLSConfigFunc()
	require.NoError(t, err)

	addr := startTLSServer(t, cfgFn)

	// Client is restricted to TLS 1.2 with the same single cipher.
	clientCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // self-signed cert used only in tests
		MaxVersion:         tls.VersionTLS12,
		CipherSuites:       []uint16{cipherID},
	}

	state, err := dialTLS(addr, clientCfg)
	require.NoError(t, err)
	require.Equal(t, cipherID, state.CipherSuite, "expected cipher %s to be negotiated", cipher)
}

// TestCustomTLSProfileCipherRejection verifies that the server rejects a
// connection when the client offers only a cipher not in the custom profile.
func TestCustomTLSProfileCipherRejection(t *testing.T) {
	// Server is configured with AES-256 only.
	setCustomProfile(t,
		[]string{"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"},
		[]string{"prime256v1"},
		"TLSv1.2",
	)

	cfgFn, err := GetTLSConfigFunc()
	require.NoError(t, err)

	addr := startTLSServer(t, cfgFn)

	// Client offers only AES-128, which the server does not allow.
	cipherID := cipherSuiteId("TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256")
	require.NotZero(t, cipherID, "cipher suite must be available on this platform")
	clientCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // self-signed cert used only in tests
		MaxVersion:         tls.VersionTLS12,
		CipherSuites:       []uint16{cipherID},
	}

	_, err = dialTLS(addr, clientCfg)
	require.Error(t, err, "connection should fail when client offers no cipher from the custom profile")
}

// TestCustomTLSProfileMinVersionEnforcement verifies that a custom profile
// configured with a TLS 1.3 minimum rejects TLS 1.2-only clients.
func TestCustomTLSProfileMinVersionEnforcement(t *testing.T) {
	setCustomProfile(t,
		[]string{"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"},
		[]string{"prime256v1"},
		"TLSv1.3",
	)

	cfgFn, err := GetTLSConfigFunc()
	require.NoError(t, err)

	addr := startTLSServer(t, cfgFn)

	// Client advertises TLS 1.2 as its maximum; server requires TLS 1.3.
	clientCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // self-signed cert used only in tests
		MaxVersion:         tls.VersionTLS12,
	}

	_, err = dialTLS(addr, clientCfg)
	require.Error(t, err, "connection should fail when server requires TLS 1.3 and client only supports TLS 1.2")
}

// TestCustomTLSProfileCurveNegotiation verifies that a connection succeeds when
// the client's curve preferences overlap with the custom profile's curve list.
func TestCustomTLSProfileCurveNegotiation(t *testing.T) {
	// Server allows only prime256v1 (P-256).
	setCustomProfile(t,
		[]string{"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"},
		[]string{"prime256v1"},
		"TLSv1.2",
	)

	cfgFn, err := GetTLSConfigFunc()
	require.NoError(t, err)

	addr := startTLSServer(t, cfgFn)

	// Client also only uses prime256v1 — there is an overlap.
	cipherID := cipherSuiteId("TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256")
	require.NotZero(t, cipherID, "cipher suite must be available on this platform")
	clientCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // self-signed cert used only in tests
		MaxVersion:         tls.VersionTLS12,
		CurvePreferences:   []tls.CurveID{tls.CurveP256},
		CipherSuites:       []uint16{cipherID},
	}

	_, err = dialTLS(addr, clientCfg)
	require.NoError(t, err)
}

// TestCustomTLSProfileCurveRejection verifies that a connection fails when the
// client's supported curves do not overlap with the custom profile's curve list.
// TLS 1.2 is used because the curve negotiation failure is deterministic there;
// TLS 1.3 can fall back via HelloRetryRequest.
func TestCustomTLSProfileCurveRejection(t *testing.T) {
	// Server allows only prime256v1 (P-256).
	setCustomProfile(t,
		[]string{"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"},
		[]string{"prime256v1"},
		"TLSv1.2",
	)

	cfgFn, err := GetTLSConfigFunc()
	require.NoError(t, err)

	addr := startTLSServer(t, cfgFn)

	// Client only supports X25519, which is not in the server's curve list.
	cipherID := cipherSuiteId("TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256")
	require.NotZero(t, cipherID, "cipher suite must be available on this platform")
	clientCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // self-signed cert used only in tests
		MaxVersion:         tls.VersionTLS12,
		CurvePreferences:   []tls.CurveID{tls.X25519},
		CipherSuites:       []uint16{cipherID},
	}

	_, err = dialTLS(addr, clientCfg)
	require.Error(t, err, "connection should fail when client and server share no common curve")
}
