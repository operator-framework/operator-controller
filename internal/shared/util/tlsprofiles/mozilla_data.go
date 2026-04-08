package tlsprofiles

// This file embeds the Mozilla SSL/TLS Configuration Guidelines JSON and parses
// it at init() time to populate the modern and intermediate TLS profiles.
// Run `make update-tls-profiles` to refresh mozilla_data.json from the upstream spec.

import (
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed mozilla_data.json
var mozillaDataJSON []byte

// skippedCiphers records cipher names from mozilla_data.json that are not
// supported by Go's crypto/tls and were omitted from the profiles.
var skippedCiphers []string

// skippedCurves records curve names from mozilla_data.json that are not
// supported by Go's crypto/tls and were omitted from the profiles.
var skippedCurves []string

var (
	modernTLSProfile       tlsProfile
	intermediateTLSProfile tlsProfile
)

type mozillaConfiguration struct {
	Ciphersuites []string `json:"ciphersuites"`
	Ciphers      struct {
		IANA []string `json:"iana"`
	} `json:"ciphers"`
	TLSCurves   []string `json:"tls_curves"`
	TLSVersions []string `json:"tls_versions"`
}

type mozillaSpec struct {
	Configurations map[string]mozillaConfiguration `json:"configurations"`
}

func init() {
	var spec mozillaSpec
	if err := json.Unmarshal(mozillaDataJSON, &spec); err != nil {
		panic(fmt.Sprintf("tlsprofiles: failed to parse embedded mozilla_data.json: %v", err))
	}

	for _, name := range []string{"modern", "intermediate"} {
		cfg, ok := spec.Configurations[name]
		if !ok {
			panic(fmt.Sprintf("tlsprofiles: profile %q not found in embedded mozilla_data.json", name))
		}

		p, ciphers, curves := parseProfile(name, cfg)
		skippedCiphers = append(skippedCiphers, ciphers...)
		skippedCurves = append(skippedCurves, curves...)

		switch name {
		case "modern":
			modernTLSProfile = p
		case "intermediate":
			intermediateTLSProfile = p
		}
	}
}

func parseProfile(name string, cfg mozillaConfiguration) (tlsProfile, []string, []string) {
	var skippedC, skippedK []string
	var cipherNums []uint16
	for _, c := range append(cfg.Ciphersuites, cfg.Ciphers.IANA...) {
		id := cipherSuiteId(c)
		if id == 0 {
			skippedC = append(skippedC, c)
			continue
		}
		cipherNums = append(cipherNums, id)
	}

	var curveNums []tls.CurveID
	for _, c := range cfg.TLSCurves {
		id := curveId(c)
		if id == 0 {
			skippedK = append(skippedK, c)
			continue
		}
		curveNums = append(curveNums, id)
	}

	if len(cfg.TLSVersions) == 0 {
		panic(fmt.Sprintf("tlsprofiles: profile %q has no tls_versions in embedded mozilla_data.json", name))
	}

	var version tlsVersion
	if err := version.Set(cfg.TLSVersions[0]); err != nil {
		panic(fmt.Sprintf("tlsprofiles: profile %q has unrecognized tls_versions[0] %q: %v", name, cfg.TLSVersions[0], err))
	}

	return tlsProfile{
		ciphers:       cipherSlice{cipherNums: cipherNums},
		curves:        curveSlice{curveNums: curveNums},
		minTLSVersion: version,
	}, skippedC, skippedK
}
