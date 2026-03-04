package upgradee2e

import (
	"log"
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/spf13/pflag"

	"github.com/operator-framework/operator-controller/test/e2e/steps"
)

var opts = godog.Options{
	Format:      "pretty",
	Paths:       []string{"features"},
	Output:      colors.Colored(os.Stdout),
	Concurrency: 1,
	NoColors:    true,
}

func init() {
	godog.BindCommandLineFlags("godog.", &opts)
}

func TestMain(m *testing.M) {
	pflag.Parse()
	opts.Paths = pflag.Args()

	sc := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			sc.Before(steps.CreateScenarioContext)
			steps.RegisterSteps(sc)
		},
		Options: &opts,
	}.Run()

	if sc != 0 {
		log.Fatalf("non-zero status returned (%d), failed to run upgrade feature tests", sc)
	}
}
