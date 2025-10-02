package tlsprofiles

import (
	"crypto/tls"
	"fmt"
	"strings"
)

var (
	configuredProfile tlsProfileName = "intermediate"
	customTLSProfile  tlsProfile     = tlsProfile{
		ciphers:       cipherSlice{},
		curves:        curveSlice{},
		minTLSVersion: tls.VersionTLS12,
	}
)

type tlsProfile struct {
	ciphers       cipherSlice
	curves        curveSlice
	minTLSVersion tlsVersion
}

// Based on compatibility levels from: https://wiki.mozilla.org/Security/Server_Side_TLS
var profiles = map[string]*tlsProfile{
	"modern":       &modernTLSProfile,
	"intermediate": &intermediateTLSProfile,
	"old":          &oldTLSProfile,
	"custom":       &customTLSProfile,
}

func findTLSProfile(profile tlsProfileName) (*tlsProfile, error) {
	p := strings.ToLower(profile.String())
	tlsProfile, ok := profiles[p]
	if !ok {
		return nil, fmt.Errorf("unknown TLS profile: %q", profile.String())
	}
	return tlsProfile, nil
}

func GetTLSConfigFunc() (func(*tls.Config), error) {
	tlsProfile, err := findTLSProfile(configuredProfile)
	if err != nil {
		return nil, err
	}
	return func(config *tls.Config) {
		config.MinVersion = uint16(tlsProfile.minTLSVersion)
		config.CipherSuites = tlsProfile.ciphers.cipherNums
		config.CurvePreferences = tlsProfile.curves.curveNums
	}, nil
}

// This function should _really_ exist in crypto/tls
// cipherSuiteId returns the cipher suite ID given a standard name
// (e.g. "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"), or 0 if no such cipher exists
func cipherSuiteId(name string) uint16 {
	for _, c := range tls.CipherSuites() {
		if c.Name == name {
			return c.ID
		}
	}
	for _, c := range tls.InsecureCipherSuites() {
		if c.Name == name {
			return c.ID
		}
	}
	return 0
}

// This is primarily so that we don't have to rewrite curve values in mozilla_data.go
const (
	X25519     tls.CurveID = tls.X25519
	prime256v1 tls.CurveID = tls.CurveP256
	secp384r1  tls.CurveID = tls.CurveP384
	secp521r1  tls.CurveID = tls.CurveP521
)

var curves = map[string]tls.CurveID{
	"X25519":     tls.X25519,
	"prime256v1": tls.CurveP256,
	"secp384r1":  tls.CurveP384,
	"secp521r1":  tls.CurveP521,
}

// Returns 0 for an invalid curve name
func curveId(name string) tls.CurveID {
	if id, ok := curves[name]; ok {
		return id
	}
	return 0
}
