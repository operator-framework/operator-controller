package e2e

import (
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	utils "github.com/operator-framework/operator-controller/internal/shared/util/testutils"
	steps2 "github.com/operator-framework/operator-controller/test/e2e/features/steps"
	"github.com/spf13/pflag"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var opts = godog.Options{
	Format:      "pretty",
	Paths:       []string{"features"},
	Output:      colors.Colored(os.Stdout),
	Concurrency: 1,
}

func init() {
	logOpts := zap.Options{
		Development: true,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logOpts)))

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
	switch sc {
	//	0 - success
	case 0:

		path := os.Getenv("E2E_SUMMARY_OUTPUT")
		if path == "" {
			fmt.Printf("Note: E2E_SUMMARY_OUTPUT is unset; skipping summary generation")
		} else {
			if err := utils.PrintSummary(path); err != nil {
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
	tc.BeforeSuite(steps2.DetectEnabledFeatureGates)
}

func InitializeScenario(sc *godog.ScenarioContext) {
	sc.Step(`^OLM is available$`, steps2.OLMisAvailable)
	sc.Step(`^bundle "([^"]+)" is installed in version "([^"]+)"$`, steps2.BundleInstalled)
	sc.Step(`^ClusterExtension is applied$`, steps2.ResourceIsApplied)
	sc.Step(`^ClusterExtension is updated$`, steps2.ResourceIsApplied)
	sc.Step(`^ClusterExtension is available$`, steps2.ClusterExtensionIsAvailable)
	sc.Step(`^ClusterExtension is rolled out$`, steps2.ClusterExtensionIsRolledOut)
	sc.Step(`^ClusterExtension reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+):$`, steps2.ClusterExtensionReportsCondition)
	sc.Step(`^ClusterExtension reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+)$`, steps2.ClusterExtensionReportsConditionWithoutMsg)
	sc.Step(`^ClusterExtension reports ([[:alnum:]]+) as ([[:alnum:]]+)$`, steps2.ClusterExtensionReportsConditionWithoutReason)
	sc.Step(`^resource "([^"]+)" is installed$`, steps2.ResourceAvailable)
	sc.Step(`^resource "([^"]+)" is available$`, steps2.ResourceAvailable)
	sc.Step(`^resource "([^"]+)" is removed$`, steps2.ResourceRemoved)
	sc.Step(`^resource is applied$`, steps2.ResourceIsApplied)
	sc.Step(`^resource apply fails with error msg containing "([^"]+)"$`, steps2.ResourceApplyFails)
	sc.Step(`^resource "([^"]+)" is eventually restored$`, steps2.ResourceRestored)
	sc.Step(`^resource "([^"]+)" matches$`, steps2.ResourceMatches)
	sc.Step(`^Service account "([^"]*)" with needed permissions is available in test namespace$`, steps2.ServiceAccountWithNeededPermissionsIsAvailableInNamespace)
	sc.Step(`^Service account "([^"]*)" in test namespace is cluster admin$`, steps2.ServiceAccountWithClusterAdminPermissionsIsAvailableInNamespace)
	sc.Step(`^Service account "([^"]+)" in test namespace has permissions to fetch "([^"]+)" metrics$`, steps2.ServiceAccountWithFetchMetricsPermissions)
	sc.Step(`^Service account "([^"]+)" sends request to "([^"]+)" endpoint of "([^"]+)" service$`, steps2.SendMetricsRequest)
	sc.Step(`^"([^"]+)" catalog is updated to version "([^"]+)"$`, steps2.CatalogIsUpdatedToVersion)
	sc.Step(`^"([^"]+)" catalog serves bundles$`, steps2.CatalogServesBundles)
	sc.Step(`^"([^"]+)" catalog image version "([^"]+)" is also tagged as "([^"]+)"$`, steps2.TagCatalogImage)
	sc.Step(`^operator "([^"]+)" target namespace is "([^"]+)"$`, steps2.OperatorTargetNamespace)
	sc.Step(`^Prometheus metrics are returned in the response$`, steps2.PrometheusMetricsAreReturned)

	sc.Before(steps2.CheckFeatureTags)
	sc.Before(steps2.CreateScenarioContext)

	sc.After(steps2.ScenarioCleanup)
}
