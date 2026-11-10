package e2e

import (
	"os"
	"testing"

	"github.com/cucumber/godog"

	"github.com/operator-framework/operator-controller/test/internal/e2e"
	objectsteps "github.com/operator-framework/operator-controller/test/internal/e2e/object-controller/steps"
	operatorsteps "github.com/operator-framework/operator-controller/test/internal/e2e/operator-controller/steps"
	"github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func TestMain(m *testing.M) {
	os.Exit(e2e.Run("features", func(tc *godog.TestSuiteContext) {
		tc.BeforeSuite(steps.BeforeSuite)
	}, func(sc *godog.ScenarioContext) {
		steps.RegisterSteps(sc)
		objectsteps.RegisterSteps(sc)
		operatorsteps.RegisterSteps(sc)
		steps.RegisterHooks(sc)
	}))
}
