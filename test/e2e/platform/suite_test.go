package e2e

import (
	"os"
	"testing"

	"github.com/cucumber/godog"

	"github.com/operator-framework/operator-controller/test/internal/e2e"
	operatorsteps "github.com/operator-framework/operator-controller/test/internal/e2e/operator-controller/steps"
	platformsteps "github.com/operator-framework/operator-controller/test/internal/e2e/platform/steps"
	"github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func TestMain(m *testing.M) {
	os.Exit(e2e.Run("features", func(tc *godog.TestSuiteContext) {
		tc.BeforeSuite(steps.BeforeSuite)
	}, func(sc *godog.ScenarioContext) {
		steps.RegisterSteps(sc)
		operatorsteps.RegisterSteps(sc)
		platformsteps.RegisterDemoSteps(sc)
		steps.RegisterHooks(sc, platformsteps.RegisterAsciiCastHooks)
	}))
}
