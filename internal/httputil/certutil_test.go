package httputil_test

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/httputil"
)

// The "good" test consists of 3 Amazon Root CAs, along with a "PRIVATE KEY" in one of the files
// The "empty" test includes a single file with no PEM contents
func TestNewCertPool(t *testing.T) {
	caDirs := []struct {
		dir string
		msg string
	}{
		{"../../testdata/certs/", `no certificates found in "../../testdata/certs/"`},
		{"../../testdata/certs/good", ""},
		{"../../testdata/certs/empty", `no certificates found in "../../testdata/certs/empty"`},
	}

	log, _ := logr.FromContext(context.Background())
	for _, caDir := range caDirs {
		t.Logf("Loading certs from %q", caDir.dir)
		pool, err := httputil.NewCertPool(caDir.dir, log)
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
