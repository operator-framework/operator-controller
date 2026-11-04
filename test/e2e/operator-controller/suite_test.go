package e2e

import (
	"os"
	"testing"

	"github.com/cucumber/godog"

	"github.com/operator-framework/operator-controller/test/e2e/shared"
	"github.com/operator-framework/operator-controller/test/e2e/shared/steps"
)

func TestMain(m *testing.M) {
	os.Exit(shared.Run("features", func(tc *godog.TestSuiteContext) {
		tc.BeforeSuite(steps.BeforeSuite)
	}, func(sc *godog.ScenarioContext) {
		steps.RegisterSteps(sc)
		steps.RegisterDemoSteps(sc)
		steps.RegisterHooks(sc)
	}))
}
