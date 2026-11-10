//go:build e2e_legacy

// This compatibility entry point is invoked explicitly as a file by downstream
// Godog jobs. The build tag excludes it from ./test/e2e/... to avoid duplicate runs.
package e2e

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/operator-framework/operator-controller/test/internal/e2e"
	"github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func TestMain(m *testing.M) {
	// Preserve legacy features/<file> arguments after features move to their owners.
	for i, arg := range os.Args[1:] {
		if arg == "features" || arg == "features/" {
			os.Args[i+1] = "."
			continue
		}
		if !strings.HasPrefix(arg, "features/") {
			continue
		}
		path, line, hasLine := strings.Cut(arg, ":")
		matches, err := filepath.Glob("*/" + path)
		if err != nil || len(matches) != 1 {
			log.Fatalf("expected one component feature for %q, found %d: %v", arg, len(matches), err)
		}
		os.Args[i+1] = matches[0]
		if hasLine {
			os.Args[i+1] += ":" + line
		}
	}
	os.Exit(e2e.Run(".", func(tc *godog.TestSuiteContext) {
		tc.BeforeSuite(steps.BeforeSuite)
	}, func(sc *godog.ScenarioContext) {
		steps.RegisterSteps(sc)
		steps.RegisterDemoSteps(sc)
		steps.RegisterHooks(sc, steps.RegisterAsciiCastHooks)
	}))
}
