//go:build e2e_legacy

// This compatibility entry point is invoked explicitly as a file by downstream
// Godog jobs. The build tag excludes it from ./test/e2e/... to avoid duplicate runs.
package e2e

import (
	"log"
	"os"
	"testing"

	"github.com/cucumber/godog"

	"github.com/operator-framework/operator-controller/test/e2e/shared"
	"github.com/operator-framework/operator-controller/test/e2e/shared/steps"
)

func TestMain(m *testing.M) {
	// Keep legacy paths such as features/install.feature relative to the suite.
	if err := os.Chdir("operator-controller"); err != nil {
		log.Fatal(err)
	}
	os.Exit(shared.Run("features", func(tc *godog.TestSuiteContext) {
		tc.BeforeSuite(steps.BeforeSuite)
	}, func(sc *godog.ScenarioContext) {
		steps.RegisterSteps(sc)
		steps.RegisterDemoSteps(sc)
		steps.RegisterHooks(sc)
	}))
}
