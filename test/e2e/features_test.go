package e2e

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/spf13/pflag"

	testutil "github.com/operator-framework/operator-controller/internal/shared/util/test"
	"github.com/operator-framework/operator-controller/test/e2e/steps"
)

var cliOpts = godog.Options{
	Concurrency: 1,
	Format:      "pretty",
	Paths:       []string{"features"},
	Output:      colors.Colored(os.Stdout),
	NoColors:    true,
}

func init() {
	godog.BindCommandLineFlags("godog.", &cliOpts)
}

func TestMain(m *testing.M) {
	// parse CLI arguments
	pflag.Parse()
	cliOpts.Paths = pflag.Args()

	if cliOpts.Tags != "" {
		fmt.Println("Note: Custom feature tags provided - disabling automatic test parallelization")
		// run tests explicitly as requested by CLI
		sc := godog.TestSuite{
			TestSuiteInitializer: InitializeSuite,
			ScenarioInitializer:  InitializeScenario,
			Options:              &cliOpts,
		}.Run()

		if sc != 0 {
			//	1 - failed
			//	2 - command line usage error
			// 128 - or higher, os signal related error exit codes
			log.Fatalf("non-zero status returned: (%d), failed to run feature tests", sc)
		}
	} else {
		executeTestsParallel()
	}

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
}

func executeTestsParallel() {
	// Create buffers to capture output for final summary
	var parallelBuf, serialBuf bytes.Buffer

	parallelOpts := cliOpts
	if !pflag.Lookup("godog.concurrency").Changed {
		// Override default concurrency value with 100; otherwise use whatever was provided by CLI
		parallelOpts.Concurrency = 100
	}
	parallelOpts.Tags = "~@Serial"
	// Write to both specified output (live to stdout, by default) and buffer (for summary)
	parallelOpts.Output = io.MultiWriter(parallelOpts.Output, &parallelBuf)
	// run tests concurrently
	scParallel := godog.TestSuite{
		TestSuiteInitializer: InitializeSuite,
		ScenarioInitializer:  InitializeScenario,
		Options:              &parallelOpts,
	}.Run()

	fmt.Println("End of parallel run - beginning serial tests")

	serialOpts := cliOpts
	serialOpts.Concurrency = 1
	serialOpts.Tags = "@Serial"
	// Write to both specified output (live to stdout, by default) and buffer (for summary)
	serialOpts.Output = io.MultiWriter(serialOpts.Output, &serialBuf)
	// run tests serially
	scSerial := godog.TestSuite{
		TestSuiteInitializer: InitializeSuite,
		ScenarioInitializer:  InitializeScenario,
		Options:              &serialOpts,
	}.Run()

	// TODO We re-print the output of any failed steps here for easier debugging. However, it would be
	// better to combine this with the E2E_SUMMARY_OUTPUT and show pass/fail + performance in one
	// markdown output then preserve the console output for local testing.

	// Print aggregated summary
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("TEST EXECUTION SUMMARY")
	fmt.Println(strings.Repeat("=", 80))

	fmt.Printf("\nParallel Tests Exit Code: %d\n", scParallel)
	if scParallel != 0 {
		failedSteps := extractFailedSteps(parallelBuf.String())
		if failedSteps != "" {
			fmt.Println("\nParallel Test Failures:")
			fmt.Println(strings.Repeat("-", 80))
			fmt.Println(failedSteps)
		}
	}

	fmt.Printf("\nSerial Tests Exit Code: %d\n", scSerial)
	if scSerial != 0 {
		failedSteps := extractFailedSteps(serialBuf.String())
		if failedSteps != "" {
			fmt.Println("\nSerial Test Failures:")
			fmt.Println(strings.Repeat("-", 80))
			fmt.Println(failedSteps)
		}
	}

	fmt.Println(strings.Repeat("=", 80))

	if scParallel != 0 || scSerial != 0 {
		//	1 - failed
		//	2 - command line usage error
		// 128 - or higher, os signal related error exit codes
		log.Fatalf("non-zero status returned; parallel: (%d), serial: (%d), failed to run feature tests", scParallel, scSerial)
	}
}

// extractFailedSteps extracts the "--- Failed steps:" section from godog output
func extractFailedSteps(output string) string {
	lines := strings.Split(output, "\n")
	var failedSection []string
	capturing := false

	for _, line := range lines {
		if strings.Contains(line, "--- Failed steps:") {
			capturing = true
		}
		if capturing {
			failedSection = append(failedSection, line)
		}
	}

	if len(failedSection) == 0 {
		return ""
	}
	return strings.Join(failedSection, "\n")
}

func InitializeSuite(tc *godog.TestSuiteContext) {
	tc.BeforeSuite(steps.BeforeSuite)
}

func InitializeScenario(sc *godog.ScenarioContext) {
	steps.RegisterSteps(sc)
	steps.RegisterHooks(sc)
}
