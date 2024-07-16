package httputil_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/httputil"
)

// The "good" test consists of 3 Amazon Root CAs, along with a "PRIVATE KEY" in one of the files
// The "bad" test consists of 2 Amazon Root CAs, the second of which is garbage, and the test fails
// The "ugly" test consists of a single file:
// - Amazon_Root_CA_1
// - garbage PEM
// - Amazon_Root_CA_3
// The error is _not_ detected because the golang standard library PEM decoder skips right over the garbage
// This demonstrates the danger of putting multiple certificates into a single file
func TestNewCertPool(t *testing.T) {
	caDirs := []struct {
		dir string
		msg string
	}{
		{"../../testdata/certs/good", ""},
		{"../../testdata/certs/bad", `error adding cert file "../../testdata/certs/bad/Amazon_Root_CA_2.pem": unable to PEM decode cert 1`},
		{"../../testdata/certs/ugly", `error adding cert file "../../testdata/certs/ugly/Amazon_Root_CA.pem": unable to PEM decode cert 2`},
		{"../../testdata/certs/ugly2", `error adding cert file "../../testdata/certs/ugly2/Amazon_Root_CA_1.pem": unable to PEM decode cert 1`},
		{"../../testdata/certs/ugly3", `error adding cert file "../../testdata/certs/ugly3/not_a_cert.pem": unable to PEM decode cert 1`},
		{"../../testdata/certs/empty", `error adding cert file "../../testdata/certs/empty/empty.pem": unable to parse cert 1: x509: malformed certificate`},
	}

	for _, caDir := range caDirs {
		t.Logf("Loading certs from %q", caDir.dir)
		pool, err := httputil.NewCertPool(caDir.dir)
		if caDir.msg == "" {
			require.NoError(t, err)
			require.NotNil(t, pool)
		} else {
			require.Error(t, err)
			require.Nil(t, pool)
			require.ErrorContains(t, err, caDir.msg)
		}
	}
}
