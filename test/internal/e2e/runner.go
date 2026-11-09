// Package e2e provides the runner used by component E2E suites.
package e2e

import (
	"fmt"
	"log"
	"os"
	"strings"

	gherkin "github.com/cucumber/gherkin/go/v26"
	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	messages "github.com/cucumber/messages/go/v21"
	"github.com/spf13/pflag"

	"github.com/operator-framework/operator-controller/test/internal/summary"
)

var opts = godog.Options{
	Format:      "pretty",
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

// Run executes a component suite using its feature directory and initializers.
// Call it from TestMain and pass the returned status to os.Exit.
func Run(featureDir string, suiteInitializer func(*godog.TestSuiteContext), scenarioInitializer func(*godog.ScenarioContext)) int {
	pflag.Parse()
	options := opts
	paths, err := featurePaths(featureDir, pflag.Args(), scenarioFilter)
	if err != nil {
		log.Print(err)
		return 1
	}
	options.Paths = paths

	status := godog.TestSuite{
		TestSuiteInitializer: suiteInitializer,
		ScenarioInitializer:  scenarioInitializer,
		Options:              &options,
	}.Run()
	if status != 0 {
		log.Printf("non-zero status returned (%d), failed to run feature tests", status)
		return 1
	}

	path := os.Getenv("E2E_SUMMARY_OUTPUT")
	if path == "" {
		fmt.Println("Note: E2E_SUMMARY_OUTPUT is unset; skipping summary generation")
	} else if err := summary.PrintSummary(path); err != nil {
		// Alert but do not fail the run if alerts are found.
		fmt.Printf("%v", err)
	}
	return 0
}

func featurePaths(featureDir string, paths []string, prefix string) ([]string, error) {
	if prefix == "" {
		if len(paths) == 0 {
			return []string{featureDir}, nil
		}
		return paths, nil
	}
	if len(paths) != 1 {
		return nil, fmt.Errorf("--e2e.scenario requires exactly one feature file path, got %d", len(paths))
	}
	lines, err := findScenarioFirstLineNumberByPrefix(paths[0], prefix)
	if err != nil {
		return nil, err
	}
	selected := make([]string, len(lines))
	for i, line := range lines {
		selected[i] = fmt.Sprintf("%s:%d", paths[0], line)
	}
	return selected, nil
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
