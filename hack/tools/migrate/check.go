package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/operator-framework/operator-controller/internal/operator-controller/migration"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check readiness and compatibility without performing migration",
	Long: `Runs all pre-migration checks (readiness and compatibility) and reports
any issues that would prevent migration. Does not modify any cluster resources.

Examples:
  migrate check -s my-operator -n operators`,
	RunE: runCheck,
}

func runCheck(cmd *cobra.Command, _ []string) error {
	c, restConfig, err := newClient()
	if err != nil {
		return err
	}

	opts := migration.Options{
		SubscriptionName:      subscriptionName,
		SubscriptionNamespace: subscriptionNamespace,
		ClusterExtensionName:  clusterExtensionName,
		InstallNamespace:      installNamespace,
	}
	opts.ApplyDefaults()

	ctx := cmd.Context()
	m := migration.NewMigrator(c, restConfig)

	fmt.Printf("\n%s%s🔍 Pre-migration checks for %s/%s%s\n", colorBold, colorCyan, subscriptionNamespace, subscriptionName, colorReset)

	// Readiness checks
	sectionHeader("Readiness Checks")
	readiness, readinessErr := m.CheckReadiness(ctx, opts)
	if readinessErr != nil {
		fail(fmt.Sprintf("Could not run readiness checks: %v", readinessErr))
	} else {
		printCheckResults(readiness.Checks)
	}

	// Profile the operator for compatibility checks
	sectionHeader("Compatibility Checks")
	_, csv, _, err := m.GetCSVAndInstallPlan(ctx, opts)
	if err != nil {
		fail(fmt.Sprintf("Could not profile operator: %v", err))
		if readinessErr != nil {
			return fmt.Errorf("readiness and profiling checks failed")
		}
		return fmt.Errorf("profiling failed: %w", err)
	}

	propsJSON := csv.Annotations["operatorframework.io/properties"]
	compat, compatErr := m.CheckCompatibility(ctx, opts, csv, propsJSON)
	if compatErr != nil {
		fail(fmt.Sprintf("Could not run compatibility checks: %v", compatErr))
	} else {
		printCheckResults(compat.Checks)
	}

	// ClusterCatalog check
	sectionHeader("ClusterCatalog Availability")
	info("Looking for serving ClusterCatalogs...")
	bundleInfo, _ := m.GetBundleInfo(ctx, opts, csv, nil)
	if bundleInfo != nil {
		catalogName, catalogErr := m.ResolveClusterCatalog(ctx, bundleInfo, restConfig)
		if catalogErr != nil {
			warn(fmt.Sprintf("No ClusterCatalog resolved: %v", catalogErr))
		} else {
			success(fmt.Sprintf("ClusterCatalog available: %s", catalogName))
		}
	}

	// Summary
	sectionHeader("Summary")
	totalFailed := 0
	if readinessErr != nil {
		totalFailed++
	} else {
		totalFailed += len(readiness.FailedChecks())
	}
	if compatErr != nil {
		totalFailed++
	} else {
		totalFailed += len(compat.FailedChecks())
	}

	if totalFailed > 0 {
		fail(fmt.Sprintf("%d issue(s) found — operator is NOT ready for migration", totalFailed))
		fmt.Println()
		return fmt.Errorf("pre-migration checks failed")
	}

	success(fmt.Sprintf("Operator %s/%s is ready for migration to OLMv1", subscriptionNamespace, subscriptionName))
	fmt.Println()
	return nil
}
