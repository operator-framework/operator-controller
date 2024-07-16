package httputil

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

var pemStart = []byte("\n-----BEGIN ")
var pemEnd = []byte("\n-----END ")
var pemEndOfLine = []byte("-----")
var colon = []byte(":")

// getLine results the first \r\n or \n delineated line from the given byte
// array. The line does not include trailing whitespace or the trailing new
// line bytes. The remainder of the byte array (also not including the new line
// bytes) is also returned and this will always be smaller than the original
// argument.
func getLine(data []byte) ([]byte, []byte) {
	i := bytes.IndexByte(data, '\n')
	var j int
	if i < 0 {
		i = len(data)
		j = i
	} else {
		j = i + 1
		if i > 0 && data[i-1] == '\r' {
			i--
		}
	}
	return bytes.TrimRight(data[0:i], " \t"), data[j:]
}

// removeSpacesAndTabs returns a copy of its input with all spaces and tabs
// removed, if there were any. Otherwise, the input is returned unchanged.
//
// The base64 decoder already skips newline characters, so we don't need to
// filter them out here.
func removeSpacesAndTabs(data []byte) []byte {
	if !bytes.ContainsAny(data, " \t") {
		// Fast path; most base64 data within PEM contains newlines, but
		// no spaces nor tabs. Skip the extra alloc and work.
		return data
	}
	result := make([]byte, len(data))
	n := 0

	for _, b := range data {
		if b == ' ' || b == '\t' {
			continue
		}
		result[n] = b
		n++
	}

	return result[0:n]
}

// This version of pem.Decode() is a bit less flexible, it will not skip over bad PEM
// It is basically the guts of pem.Decode() inside the outer for loop, with error
// returns rather than continues
func pemDecode(data []byte) (*pem.Block, []byte) {
	// pemStart begins with a newline. However, at the very beginning of
	// the byte array, we'll accept the start string without it.
	rest := data
	if bytes.HasPrefix(rest, pemStart[1:]) {
		rest = rest[len(pemStart)-1:]
	} else if _, after, ok := bytes.Cut(rest, pemStart); ok {
		rest = after
	} else {
		return nil, data
	}

	var typeLine []byte
	typeLine, rest = getLine(rest)
	if !bytes.HasSuffix(typeLine, pemEndOfLine) {
		return nil, data
	}
	typeLine = typeLine[0 : len(typeLine)-len(pemEndOfLine)]

	p := &pem.Block{
		Headers: make(map[string]string),
		Type:    string(typeLine),
	}

	for {
		// This loop terminates because getLine's second result is
		// always smaller than its argument.
		if len(rest) == 0 {
			return nil, data
		}
		line, next := getLine(rest)

		key, val, ok := bytes.Cut(line, colon)
		if !ok {
			break
		}

		key = bytes.TrimSpace(key)
		val = bytes.TrimSpace(val)
		p.Headers[string(key)] = string(val)
		rest = next
	}

	var endIndex, endTrailerIndex int

	// If there were no headers, the END line might occur
	// immediately, without a leading newline.
	if len(p.Headers) == 0 && bytes.HasPrefix(rest, pemEnd[1:]) {
		endIndex = 0
		endTrailerIndex = len(pemEnd) - 1
	} else {
		endIndex = bytes.Index(rest, pemEnd)
		endTrailerIndex = endIndex + len(pemEnd)
	}

	if endIndex < 0 {
		return nil, data
	}

	// After the "-----" of the ending line, there should be the same type
	// and then a final five dashes.
	endTrailer := rest[endTrailerIndex:]
	endTrailerLen := len(typeLine) + len(pemEndOfLine)
	if len(endTrailer) < endTrailerLen {
		return nil, data
	}

	restOfEndLine := endTrailer[endTrailerLen:]
	endTrailer = endTrailer[:endTrailerLen]
	if !bytes.HasPrefix(endTrailer, typeLine) ||
		!bytes.HasSuffix(endTrailer, pemEndOfLine) {
		return nil, data
	}

	// The line must end with only whitespace.
	if s, _ := getLine(restOfEndLine); len(s) != 0 {
		return nil, data
	}

	base64Data := removeSpacesAndTabs(rest[:endIndex])
	p.Bytes = make([]byte, base64.StdEncoding.DecodedLen(len(base64Data)))
	n, err := base64.StdEncoding.Decode(p.Bytes, base64Data)
	if err != nil {
		return nil, data
	}
	p.Bytes = p.Bytes[:n]

	// the -1 is because we might have only matched pemEnd without the
	// leading newline if the PEM block was empty.
	_, rest = getLine(rest[endIndex+len(pemEnd)-1:])
	return p, rest
}

// This version of (*x509.CertPool).AppendCertsFromPEM() will error out if parsing fails
func appendCertsFromPEM(s *x509.CertPool, pemCerts []byte) error {
	n := 1
	for len(pemCerts) > 0 {
		var block *pem.Block
		block, pemCerts = pemDecode(pemCerts)
		if block == nil {
			return fmt.Errorf("unable to PEM decode cert %d", n)
		}
		// ignore non-certificates (e.g. keys)
		if block.Type != "CERTIFICATE" {
			continue
		}
		if len(block.Headers) != 0 {
			// This is a cert, but we're ignoring it, so bump the counter
			n++
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("unable to parse cert %d: %w", n, err)
		}
		// no return values - panics or always succeeds
		s.AddCert(cert)
		n++
	}

	return nil
}

func NewCertPool(caDir string) (*x509.CertPool, error) {
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
	for _, e := range dirEntries {
		if e.IsDir() {
			continue
		}
		file := filepath.Join(caDir, e.Name())
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("error reading cert file %q: %w", file, err)
		}
		err = appendCertsFromPEM(caCertPool, data)
		if err != nil {
			return nil, fmt.Errorf("error adding cert file %q: %w", file, err)
		}
	}

	return caCertPool, nil
}
