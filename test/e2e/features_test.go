package e2e

import (
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	gherkin "github.com/cucumber/gherkin/go/v26"
	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	messages "github.com/cucumber/messages/go/v21"
	"github.com/spf13/pflag"

	"github.com/operator-framework/operator-controller/test/e2e/steps"
	"github.com/operator-framework/operator-controller/test/internal/summary"
)

var opts = godog.Options{
	Format:      "pretty",
	Paths:       []string{"features"},
	Output:      colors.Colored(os.Stdout),
	Concurrency: 1,
	NoColors:    true,
	Strict:      true,
}

var scenarioFilter string

func init() {
	godog.BindCommandLineFlags("godog.", &opts)
	pflag.StringVar(&scenarioFilter, "e2e.scenario", "", "scenario name prefix (case-insensitive)")
}

func TestMain(m *testing.M) {
	// parse CLI arguments
	pflag.Parse()
	opts.Paths = pflag.Args()

	if scenarioFilter != "" {
		if len(opts.Paths) != 1 {
			log.Fatalf("--e2e.scenario requires exactly one feature file path, got %d", len(opts.Paths))
		}
		lines, err := findScenarioFirstLineNumberByPrefix(opts.Paths[0], scenarioFilter)
		if err != nil {
			log.Fatal(err)
		}
		basePath := opts.Paths[0]
		opts.Paths = make([]string, len(lines))
		for i, line := range lines {
			opts.Paths[i] = fmt.Sprintf("%s:%d", basePath, line)
		}
	}

	// run tests
	sc := godog.TestSuite{
		TestSuiteInitializer: InitializeSuite,
		ScenarioInitializer:  InitializeScenario,
		Options:              &opts,
	}.Run()

	switch sc {
	//	0 - success
	case 0:

		path := os.Getenv("E2E_SUMMARY_OUTPUT")
		if path == "" {
			fmt.Println("Note: E2E_SUMMARY_OUTPUT is unset; skipping summary generation")
		} else {
			if err := summary.PrintSummary(path); err != nil {
				// Alert but do not fail the run if alerts are found
				fmt.Printf("%v", err)
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

func findScenarioFirstLineNumberByPrefix(featurePath, prefix string) ([]int, error) {
	f, err := os.Open(featurePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", featurePath, err)
	}
	defer f.Close()

	doc, err := gherkin.ParseGherkinDocument(f, (&messages.Incrementing{}).NewId)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", featurePath, err)
	}

	if doc.Feature == nil {
		return nil, fmt.Errorf("no Feature found in %s", featurePath)
	}

	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, fmt.Errorf("scenario prefix must not be empty")
	}
	prefix = strings.ToLower(prefix)
	var matches []int
	var allNames []string

	matchScenario := func(sc *messages.Scenario) {
		allNames = append(allNames, sc.Name)
		if strings.HasPrefix(strings.ToLower(sc.Name), prefix) {
			matches = append(matches, int(sc.Location.Line))
		}
	}
	for _, child := range doc.Feature.Children {
		if child.Scenario != nil {
			matchScenario(child.Scenario)
		}
		if child.Rule != nil {
			for _, rc := range child.Rule.Children {
				if rc.Scenario != nil {
					matchScenario(rc.Scenario)
				}
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no scenario matching prefix %q in %s\navailable scenarios:\n  %s",
			prefix, featurePath, strings.Join(allNames, "\n  "))
	}
	return matches, nil
}

func InitializeSuite(tc *godog.TestSuiteContext) {
	tc.BeforeSuite(steps.BeforeSuite)
}

func InitializeScenario(sc *godog.ScenarioContext) {
	steps.RegisterSteps(sc)
	steps.RegisterHooks(sc)
}
