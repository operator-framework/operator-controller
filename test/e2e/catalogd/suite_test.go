package e2e

import (
	"os"
	"testing"

	"github.com/cucumber/godog"

	"github.com/operator-framework/operator-controller/test/internal/e2e"
	catalogsteps "github.com/operator-framework/operator-controller/test/internal/e2e/catalogd/steps"
	"github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func TestMain(m *testing.M) {
	os.Exit(e2e.Run("features", func(tc *godog.TestSuiteContext) {
		tc.BeforeSuite(steps.BeforeSuite)
	}, func(sc *godog.ScenarioContext) {
		steps.RegisterSteps(sc)
		catalogsteps.RegisterSteps(sc)
		steps.RegisterDemoSteps(sc)
		catalogsteps.RegisterHooks(sc)
		steps.RegisterHooks(sc)
	}))
}
