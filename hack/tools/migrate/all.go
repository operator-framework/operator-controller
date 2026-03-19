package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"

	"github.com/operator-framework/operator-controller/internal/operator-controller/migration"
)

var allCmd = &cobra.Command{
	Use:   "all",
	Short: "Discover and migrate all eligible OLMv0 operators to OLMv1",
	Long: `Scans the cluster for all OLMv0 Subscriptions, checks each for migration
eligibility, presents a summary, and migrates the eligible operators one by one.

Examples:
  # Interactive — review and approve each operator
  migrate all

  # Non-interactive — migrate all eligible operators
  migrate all -y`,
	RunE: runAll,
}

func runAll(cmd *cobra.Command, _ []string) error {
	c, restCfg, err := newClient()
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	m := migration.NewMigrator(c, restCfg)
	m.Progress = progressFunc

	// Phase 1: Scan
	fmt.Printf("\n%s%s🔎 Scanning cluster for OLMv0 Subscriptions...%s\n", colorBold, colorCyan, colorReset)
	startProgress()
	results, err := m.ScanAllSubscriptions(ctx)
	clearProgress()
	if err != nil {
		fail(fmt.Sprintf("Failed to scan Subscriptions: %v", err))
		return err
	}

	if len(results) == 0 {
		info("No Subscriptions found on the cluster.")
		return nil
	}

	// Phase 2: Display results
	var eligible, ineligible []migration.OperatorScanResult
	for _, r := range results {
		if r.Eligible {
			eligible = append(eligible, r)
		} else {
			ineligible = append(ineligible, r)
		}
	}

	sectionHeader(fmt.Sprintf("Scan Results (%d Subscriptions found)", len(results)))

	if len(eligible) > 0 {
		fmt.Printf("\n  %s%sEligible for migration (%d):%s\n", colorBold, colorGreen, len(eligible), colorReset)
		for i, r := range eligible {
			fmt.Printf("  %s%d)%s %s%s%s/%s%s (package: %s, version: %s)\n",
				colorGreen, i+1, colorReset,
				colorBold, r.SubscriptionNamespace, colorReset,
				r.SubscriptionName, colorReset,
				r.PackageName, r.Version)
		}
	}

	if len(ineligible) > 0 {
		fmt.Printf("\n  %s%sNot eligible (%d):%s\n", colorBold, colorRed, len(ineligible), colorReset)
		for _, r := range ineligible {
			reason := summarizeIneligibility(r)
			fmt.Printf("  %s✗%s %s/%s (package: %s) — %s%s%s\n",
				colorRed, colorReset,
				r.SubscriptionNamespace, r.SubscriptionName,
				r.PackageName,
				colorDim, reason, colorReset)
		}
	}

	if len(eligible) == 0 {
		fmt.Println()
		warn("No operators are eligible for migration.")
		return nil
	}

	// Phase 3: Confirmation
	if !autoApprove {
		fmt.Printf("\n%s🔄 Migrate %d eligible operator(s) to OLMv1? [y/N]: %s", colorYellow, len(eligible), colorReset)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			warn("Migration cancelled by user")
			return nil
		}
	}

	// Phase 4: Migrate each operator
	var succeeded, failed int
	for i, r := range eligible {
		fmt.Printf("\n%s%s════════════════════════════════════════════════════════════%s\n",
			colorBold, colorCyan, colorReset)
		fmt.Printf("%s%s  [%d/%d] Migrating %s/%s (%s@%s)%s\n",
			colorBold, colorCyan, i+1, len(eligible),
			r.SubscriptionNamespace, r.SubscriptionName,
			r.PackageName, r.Version, colorReset)
		fmt.Printf("%s%s════════════════════════════════════════════════════════════%s\n",
			colorBold, colorCyan, colorReset)

		if err := migrateSingle(ctx, m, r, restCfg); err != nil {
			fail(fmt.Sprintf("Migration failed: %v", err))
			failed++

			if !autoApprove && i < len(eligible)-1 {
				fmt.Printf("\n  %sContinue with remaining operators? [y/N]: %s", colorYellow, colorReset)
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					warn("Remaining migrations cancelled")
					break
				}
			}
		} else {
			succeeded++
		}
	}

	// Phase 5: Summary
	fmt.Printf("\n%s%s════════════════════════════════════════════════════════════%s\n",
		colorBold, colorCyan, colorReset)
	sectionHeader("Migration Summary")
	if succeeded > 0 {
		success(fmt.Sprintf("%d operator(s) migrated successfully", succeeded))
	}
	if failed > 0 {
		fail(fmt.Sprintf("%d operator(s) failed to migrate", failed))
	}
	if len(ineligible) > 0 {
		info(fmt.Sprintf("%d operator(s) were not eligible", len(ineligible)))
	}
	fmt.Println()

	if failed > 0 {
		return fmt.Errorf("%d migration(s) failed", failed)
	}
	return nil
}

func migrateSingle(ctx context.Context, m *migration.Migrator, r migration.OperatorScanResult, restCfg *rest.Config) error {
	opts := migration.Options{
		SubscriptionName:      r.SubscriptionName,
		SubscriptionNamespace: r.SubscriptionNamespace,
	}
	opts.ApplyDefaults()

	// Profile
	sub, csv, ip, err := m.GetCSVAndInstallPlan(ctx, opts)
	if err != nil {
		return fmt.Errorf("profiling failed: %w", err)
	}

	bundleInfo, err := m.GetBundleInfo(ctx, opts, csv, ip)
	if err != nil {
		return fmt.Errorf("bundle info failed: %w", err)
	}
	_ = sub // already checked in scan

	detail("Package:", bundleInfo.PackageName)
	detail("Version:", bundleInfo.Version)

	// Catalog resolution
	info("Resolving ClusterCatalog...")
	csImage, _ := m.GetCatalogSourceImage(ctx, bundleInfo.CatalogSourceRef)
	if csImage != "" {
		bundleInfo.CatalogSourceImage = csImage
	}

	startProgress()
	catalogName, err := m.ResolveClusterCatalog(ctx, bundleInfo, restCfg)
	clearProgress()
	if err != nil {
		var notFound *migration.PackageNotFoundError
		if errors.As(err, &notFound) && bundleInfo.CatalogSourceImage != "" {
			warn(err.Error())
			catalogName, err = promptCreateCatalog(ctx, m, bundleInfo, restCfg)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("catalog resolution failed: %w", err)
		}
	}
	bundleInfo.ResolvedCatalogName = catalogName
	success(fmt.Sprintf("Using catalog: %s", catalogName))

	// Collect
	info("Collecting resources...")
	objects, err := m.CollectResources(ctx, opts, csv, ip, bundleInfo.PackageName)
	if err != nil {
		return fmt.Errorf("resource collection failed: %w", err)
	}
	bundleInfo.CollectedObjects = objects
	success(fmt.Sprintf("Collected %d resources", len(objects)))

	// Backup
	backup, err := m.BackupResources(ctx, opts, csv)
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	if err := backup.SaveToDisk("."); err != nil {
		warn(fmt.Sprintf("Could not save backup to disk: %v", err))
	} else {
		info(fmt.Sprintf("Backup: %s", backup.Dir))
	}

	// Prepare
	info("Removing OLMv0 management (orphan cascade)...")
	if err := m.PrepareForMigration(ctx, opts, csv); err != nil {
		fail("Preparation failed, recovering...")
		startProgress()
		if recoverErr := m.RecoverFromBackup(ctx, opts, backup); recoverErr != nil {
			clearProgress()
			return fmt.Errorf("preparation failed: %w; recovery also failed: %v", err, recoverErr)
		}
		clearProgress()
		return fmt.Errorf("preparation failed (recovered): %w", err)
	}
	success("OLMv0 management removed")

	// CER
	info("Creating ClusterExtensionRevision...")
	startProgress()
	if err := m.CreateClusterExtensionRevision(ctx, opts, bundleInfo); err != nil {
		clearProgress()
		fail("CER failed, recovering...")
		startProgress()
		if recoverErr := m.RecoverBeforeCE(ctx, opts, backup); recoverErr != nil {
			clearProgress()
			return fmt.Errorf("CER creation failed: %w; recovery also failed: %v", err, recoverErr)
		}
		clearProgress()
		return fmt.Errorf("CER creation failed (recovered): %w", err)
	}
	clearProgress()
	success(fmt.Sprintf("CER %s-1 available", opts.ClusterExtensionName))

	// CE
	info("Creating ClusterExtension...")
	startProgress()
	if err := m.CreateClusterExtension(ctx, opts, bundleInfo); err != nil {
		clearProgress()
		return fmt.Errorf("CE creation failed: %w", err)
	}
	clearProgress()
	success(fmt.Sprintf("CE %s installed", opts.ClusterExtensionName))

	// Cleanup
	info("Cleaning up OLMv0 resources...")
	cleanupResult := m.CleanupOLMv0Resources(ctx, opts, bundleInfo.PackageName, csv.Name)
	for _, action := range cleanupResult.Actions {
		switch {
		case action.Skipped:
			info(fmt.Sprintf("⏭️  %s", action.Description))
		case action.Error != nil:
			warn(fmt.Sprintf("%s: %v", action.Description, action.Error))
		case action.Succeeded:
			success(action.Description)
		}
	}

	banner(fmt.Sprintf("%s migrated successfully!", bundleInfo.PackageName))
	return nil
}

func summarizeIneligibility(r migration.OperatorScanResult) string {
	if r.Error != nil {
		return r.Error.Error()
	}
	if len(r.FailedChecks) > 0 {
		reasons := make([]string, len(r.FailedChecks))
		for i, c := range r.FailedChecks {
			reasons[i] = fmt.Sprintf("%s: %s", c.Name, c.Message)
		}
		return strings.Join(reasons, "; ")
	}
	return "unknown"
}
