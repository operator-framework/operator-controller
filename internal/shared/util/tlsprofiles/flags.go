package tlsprofiles

import (
	"bytes"
	"crypto/tls"
	"encoding/csv"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/pflag"
)

func AddFlags(fs *pflag.FlagSet) {
	fs.Var(&configuredProfile, "tls-profile", "The TLS profile to use. One of "+fmt.Sprintf("%v", slices.Sorted(maps.Keys(profiles))))
	fs.Var(&customTLSProfile.ciphers, "tls-custom-ciphers", "List of ciphers to be used with the custom TLS profile. Use Go-language cipher names")
	fs.Var(&customTLSProfile.curves, "tls-custom-curves", "List of curves to be used with the custom TLS profile. Values may consist of "+fmt.Sprintf("%v", slices.Sorted(maps.Keys(curves))))
	fs.Var(&customTLSProfile.minTLSVersion, "tls-custom-version", "The TLS version to be used with the custom TLS profile. One of "+fmt.Sprintf("%v", slices.Sorted(maps.Keys(tlsVersions))))
}

// Definition of the type for `--tls-profile`
type tlsProfileName string

func (p *tlsProfileName) String() string {
	return string(*p)
}

func (p *tlsProfileName) Type() string {
	return "string"
}

func (p *tlsProfileName) Set(value string) error {
	newValue := tlsProfileName(value)
	_, err := findTLSProfile(newValue)
	if err != nil {
		return err
	}
	*p = newValue
	return nil
}

// Definition of the type for `--tls-custom-ciphers`
type cipherSlice struct {
	cipherNums  []uint16
	cipherNames []string
}

func readAsCSV(val string) ([]string, error) {
	if val == "" {
		return []string{}, nil
	}
	stringReader := strings.NewReader(val)
	csvReader := csv.NewReader(stringReader)
	return csvReader.Read()
}

func writeAsCSV(vals []string) (string, error) {
	b := &bytes.Buffer{}
	w := csv.NewWriter(b)
	err := w.Write(vals)
	if err != nil {
		return "", err
	}
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n"), nil
}

func (s *cipherSlice) Set(val string) error {
	v, err := readAsCSV(val)
	if err != nil {
		return err
	}
	return s.Replace(v)
}

func (s *cipherSlice) Type() string {
	return "stringSlice"
}

func (s *cipherSlice) String() string {
	str, _ := writeAsCSV(s.cipherNames)
	return "[" + str + "]"
}

func (s *cipherSlice) Append(val string) error {
	num := cipherSuiteId(val)
	if num == 0 {
		return fmt.Errorf("unknown cipher %q", val)
	}
	s.cipherNums = append(s.cipherNums, num)
	s.cipherNames = append(s.cipherNames, val)
	return nil
}

func (s *cipherSlice) Replace(val []string) error {
	s.cipherNames = make([]string, 0, len(val))
	s.cipherNums = make([]uint16, 0, len(val))
	for _, cipher := range val {
		if err := s.Append(cipher); err != nil {
			return err
		}
	}
	return nil
}

func (s *cipherSlice) GetSlice() []string {
	return s.cipherNames
}

// Definition of the type for `--tls-custom-curves`
type curveSlice struct {
	curveNums  []tls.CurveID
	curveNames []string
}

func (s *curveSlice) Set(val string) error {
	v, err := readAsCSV(val)
	if err != nil {
		return err
	}
	return s.Replace(v)
}

func (s *curveSlice) Type() string {
	return "stringSlice"
}

func (s *curveSlice) String() string {
	str, _ := writeAsCSV(s.curveNames)
	return "[" + str + "]"
}

func (s *curveSlice) Append(val string) error {
	num := curveId(val)
	if num == 0 {
		return fmt.Errorf("unknown curve %q", val)
	}
	s.curveNums = append(s.curveNums, num)
	s.curveNames = append(s.curveNames, val)
	return nil
}

func (s *curveSlice) Replace(val []string) error {
	s.curveNames = make([]string, 0, len(val))
	s.curveNums = make([]tls.CurveID, 0, len(val))
	for _, curve := range val {
		if err := s.Append(curve); err != nil {
			return err
		}
	}
	return nil
}

func (s *curveSlice) GetSlice() []string {
	return s.curveNames
}

// Definition of the type for `--tls-custom-version`
type tlsVersion uint16

var tlsVersions = map[string]uint16{
	"TLSv1.0": tls.VersionTLS10,
	"TLSv1.1": tls.VersionTLS11,
	"TLSv1.2": tls.VersionTLS12,
	"TLSv1.3": tls.VersionTLS13,
}

func (v *tlsVersion) String() string {
	for s, n := range tlsVersions {
		if *v == tlsVersion(n) {
			return s
		}
	}
	return ""
}

func (v *tlsVersion) Type() string {
	return "string"
}

func (v *tlsVersion) Set(value string) error {
	n, ok := tlsVersions[value]
	if !ok {
		return fmt.Errorf("unknown TLS version: %q", value)
	}
	*v = tlsVersion(n)
	return nil
}
