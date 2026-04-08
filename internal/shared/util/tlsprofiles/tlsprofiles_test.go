package tlsprofiles

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetTLSConfigFunc(t *testing.T) {
	f, err := GetTLSConfigFunc()
	require.NoError(t, err)
	require.NotNil(t, f)

	// Set an invalid profile and restore afterwards
	orig := configuredProfile
	t.Cleanup(func() { configuredProfile = orig })
	configuredProfile = "does-not-exist"
	f, err = GetTLSConfigFunc()
	require.Error(t, err)
	require.Nil(t, f)
}

func TestCipherStuiteId(t *testing.T) {
	tests := []struct {
		name   string
		result uint16
	}{
		{"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA", 0xC009},
		{"unknown-cipher", 0},
		{"TLS_RSA_WITH_3DES_EDE_CBC_SHA", 0x000A}, // Insecure cipher
		{"DHE-RSA-AES128-SHA256", 0},              // Valid OpenSSL cipher, not implemented
	}

	for _, test := range tests {
		v := cipherSuiteId(test.name)
		require.Equal(t, test.result, v)
	}
}

func TestSetProfileName(t *testing.T) {
	var profile tlsProfileName

	tests := []struct {
		name   string
		result bool
	}{
		{"modern", true},
		{"intermediate", true},
		{"old", true},
		{"custom", true},
		{"does-not-exist", false},
	}

	for _, test := range tests {
		err := (&profile).Set(test.name)
		if !test.result {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestSetCustomCipher(t *testing.T) {
	var ciphers cipherSlice

	tests := []struct {
		name   string
		result bool
	}{
		{"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA", true},
		{"unknown-cipher", false},
		{"TLS_RSA_WITH_3DES_EDE_CBC_SHA", true},                                      // Insecure cipher
		{"DHE-RSA-AES128-SHA256", false},                                             // Valid OpenSSL cipher, not implemented
		{"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,TLS_RSA_WITH_3DES_EDE_CBC_SHA", true}, // Multiple
	}

	for _, test := range tests {
		err := ciphers.Set(test.name)
		if test.result {
			require.NoError(t, err)
			require.Equal(t, "["+test.name+"]", ciphers.String())
		} else {
			require.Error(t, err)
		}
	}
}

func TestSetCustomCurves(t *testing.T) {
	var curves curveSlice

	tests := []struct {
		name   string
		result bool
	}{
		{"X25519MLKEM768", true}, // Post-quantum hybrid curve (Go 1.24+)
		{"X25519", true},
		{"prime256v1", true},
		{"secp384r1", true},
		{"secp521r1", true},
		{"unknown-cuve", false},
		{"X448", false},             // Valid OpenSSL curve, not implemented
		{"X25519,prime256v1", true}, // Multiple
	}

	for _, test := range tests {
		err := curves.Set(test.name)
		if test.result {
			require.NoError(t, err)
			require.Equal(t, "["+test.name+"]", curves.String())
		} else {
			require.Error(t, err)
		}
	}
}

func TestSetCustomVersion(t *testing.T) {
	var version tlsVersion

	tests := []struct {
		name   string
		result bool
	}{
		{"TLSv1.0", true},
		{"TLSv1.1", true},
		{"TLSv1.2", true},
		{"TLSv1.3", true},
		{"unknown-version", false},
	}

	for _, test := range tests {
		err := version.Set(test.name)
		if test.result {
			require.NoError(t, err)
			require.Equal(t, test.name, version.String())
		} else {
			require.Error(t, err)
		}
	}
}

func TestOldProfileMinVersion(t *testing.T) {
	require.EqualValues(t, tls.VersionTLS10, oldTLSProfile.minTLSVersion)
}

func TestOldProfileCiphers(t *testing.T) {
	ciphers := oldTLSProfile.ciphers.cipherNums
	// v5.8 old profile has exactly 21 ciphers
	require.Len(t, ciphers, 21, "old profile cipher count changed unexpectedly")
	// Legacy CBC cipher present in old but not modern/intermediate
	require.Contains(t, ciphers, tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA)
	// Most legacy cipher (3DES insecure)
	require.Contains(t, ciphers, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA)
	// TLS 1.3 cipher shared with all profiles
	require.Contains(t, ciphers, tls.TLS_AES_128_GCM_SHA256)
}

func TestProfilesMapCompleteness(t *testing.T) {
	for _, name := range []string{"modern", "intermediate", "old", "custom"} {
		p, err := findTLSProfile(tlsProfileName(name))
		require.NoErrorf(t, err, "profile %q must be in profiles map", name)
		require.NotNilf(t, p, "profile %q must not be nil", name)
	}
	require.GreaterOrEqual(t, len(profiles), 4, "profiles map must contain at least the required built-in profiles")
	_, err := findTLSProfile(tlsProfileName("does-not-exist"))
	require.Error(t, err, "looking up a non-existent profile must return an error")
}
