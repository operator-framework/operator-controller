package e2e

import (
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/spf13/pflag"

	testutil "github.com/operator-framework/operator-controller/internal/shared/util/test"
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
	// parse CLI arguments
	pflag.Parse()
	opts.Paths = pflag.Args()

	// run tests
	sc := godog.TestSuite{
		TestSuiteInitializer: InitializeSuite,
		ScenarioInitializer:  InitializeScenario,
		Options:              &opts,
	}.Run()

	if st := m.Run(); st > sc {
		sc = st
	}
	switch sc {
	//	0 - success
	case 0:

		path := os.Getenv("E2E_SUMMARY_OUTPUT")
		if path == "" {
			fmt.Println("Note: E2E_SUMMARY_OUTPUT is unset; skipping summary generation")
		} else {
			if err := testutil.PrintSummary(path); err != nil {
				// Fail the run if alerts are found
				fmt.Printf("%v", err)
				os.Exit(1)
			}
		}
		return

	//	1 - failed
	//	2 - command line usage error
	// 128 - or higher, os signal related error exit codes
	default:
		log.Fatalf("non-zero status returned (%d), failed to run feature tests", sc)
	}
}

func InitializeSuite(tc *godog.TestSuiteContext) {
	tc.BeforeSuite(steps.BeforeSuite)
}

func InitializeScenario(sc *godog.ScenarioContext) {
	steps.RegisterSteps(sc)
	steps.RegisterHooks(sc)
}
