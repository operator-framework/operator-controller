package e2e_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/test/internal/summary"
)

func TestSummaryOutsideSuiteDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/alerts":
			fmt.Fprint(w, `{"status":"success","data":{"alerts":[]}}`)
		case "/api/v1/query":
			fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1,"1"]]}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("PROMETHEUS_URL", server.URL)

	path := filepath.Join(t.TempDir(), "summary.md")
	require.NoError(t, summary.PrintSummary(path))
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(content), "# E2E Summary")
	require.Contains(t, string(content), "operator-controller")
	require.Contains(t, string(content), "catalogd")
	require.Contains(t, string(content), "xychart-beta")
}
