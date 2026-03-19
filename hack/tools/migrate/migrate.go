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

func runMigrate(cmd *cobra.Command, _ []string) error {
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
	m.Progress = progressFunc

	// Step 1: Profile (RFC Step 1)
	stepHeader(1, "Profiling operator")
	info("Reading Subscription, CSV, and InstallPlan...")
	sub, csv, ip, err := m.GetCSVAndInstallPlan(ctx, opts)
	if err != nil {
		fail("Failed to profile operator")
		return fmt.Errorf("failed to profile operator: %w", err)
	}

	bundleInfo, err := m.GetBundleInfo(ctx, opts, csv, ip)
	if err != nil {
		fail("Failed to get bundle info")
		return fmt.Errorf("failed to get bundle info: %w", err)
	}
	detail("Package:", bundleInfo.PackageName)
	detail("Version:", bundleInfo.Version)
	detail("Bundle:", bundleInfo.BundleName)
	detail("Channel:", valueOrDefault(bundleInfo.Channel, "(default)"))
	detail("CatalogSource:", fmt.Sprintf("%s/%s", bundleInfo.CatalogSourceRef.Namespace, bundleInfo.CatalogSourceRef.Name))
	detail("Sub State:", fmt.Sprintf("%s (installed: %s)", sub.Status.State, sub.Status.InstalledCSV))
	detail("CSV Phase:", fmt.Sprintf("%s (%s)", csv.Status.Phase, csv.Status.Reason))
	if bundleInfo.ManualApproval {
		detail("Approval:", "Manual (version will be pinned)")
	} else {
		detail("Approval:", "Automatic (upgrades will continue)")
	}
	success("Operator profiled")

	// Step 2: Readiness and Compatibility (RFC Step 2)
	stepHeader(2, "Checking readiness and compatibility")

	sectionHeader("Readiness")
	readiness, err := m.CheckReadiness(ctx, opts)
	if err != nil {
		fail(fmt.Sprintf("Error: %v", err))
		return fmt.Errorf("readiness check failed: %w", err)
	}
	printCheckResults(readiness.Checks)

	sectionHeader("Compatibility")
	propsJSON := csv.Annotations["operatorframework.io/properties"]
	compat, err := m.CheckCompatibility(ctx, opts, csv, propsJSON)
	if err != nil {
		fail(fmt.Sprintf("Error: %v", err))
		return fmt.Errorf("compatibility check error: %w", err)
	}
	printCheckResults(compat.Checks)

	allFailed := append(readiness.FailedChecks(), compat.FailedChecks()...)
	if len(allFailed) > 0 {
		return fmt.Errorf("operator is not eligible for migration (%d checks failed)", len(allFailed))
	}

	// Step 3: Determine target ClusterCatalog (RFC Step 3)
	stepHeader(3, "Determining target ClusterCatalog")

	info("Looking up CatalogSource image...")
	csImage, err := m.GetCatalogSourceImage(ctx, bundleInfo.CatalogSourceRef)
	if err != nil {
		warn(fmt.Sprintf("Could not get CatalogSource image: %v", err))
	} else {
		bundleInfo.CatalogSourceImage = csImage
		detail("Catalog image:", csImage)
	}

	info("Querying available ClusterCatalogs for package content...")
	startProgress()
	catalogName, err := m.ResolveClusterCatalog(ctx, bundleInfo, restConfig)
	clearProgress()
	if err != nil {
		var notFound *migration.PackageNotFoundError
		if errors.As(err, &notFound) && bundleInfo.CatalogSourceImage != "" {
			warn(err.Error())
			catalogName, err = promptCreateCatalog(ctx, m, bundleInfo, restConfig)
			if err != nil {
				return err
			}
		} else {
			fail(fmt.Sprintf("Failed to resolve ClusterCatalog: %v", err))
			return fmt.Errorf("failed to resolve ClusterCatalog: %w", err)
		}
	}
	bundleInfo.ResolvedCatalogName = catalogName
	success(fmt.Sprintf("Selected ClusterCatalog: %s", catalogName))

	// Step 5: Collect resources (done before Step 4 to preserve ownerRef data)
	stepHeader(5, "Collecting operator resources")
	info("Scanning Operator CR, owner labels, ownerRefs, and InstallPlan steps...")
	objects, err := m.CollectResources(ctx, opts, csv, ip, bundleInfo.PackageName)
	if err != nil {
		fail(fmt.Sprintf("Failed to collect resources: %v", err))
		return fmt.Errorf("failed to collect resources: %w", err)
	}
	bundleInfo.CollectedObjects = objects

	// Group by kind
	kindCounts := make(map[string]int)
	for _, obj := range objects {
		kindCounts[obj.GetKind()]++
	}
	success(fmt.Sprintf("Found %d resources across %d kinds", len(objects), len(kindCounts)))
	for _, obj := range objects {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = "(cluster)"
		}
		resource(obj.GetKind(), ns, obj.GetName())
	}

	// Confirmation
	if !autoApprove {
		fmt.Printf("\n%s🔄 Proceed with migration of %s%s%s to OLMv1? [y/N]: %s",
			colorYellow, colorBold, opts.SubscriptionName, colorYellow, colorReset)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			warn("Migration cancelled by user")
			return nil
		}
	}

	// Step 4: Prepare — backup and remove OLMv0 management (RFC Step 4)
	stepHeader(4, "Preparing operator for migration")

	info("Backing up Subscription and CSV for recovery...")
	backup, err := m.BackupResources(ctx, opts, csv)
	if err != nil {
		fail(fmt.Sprintf("Failed to backup resources: %v", err))
		return fmt.Errorf("failed to backup resources: %w", err)
	}
	if err := backup.SaveToDisk("."); err != nil {
		warn(fmt.Sprintf("Could not save backup to disk: %v", err))
	} else {
		success(fmt.Sprintf("Backup saved to %s", backup.Dir))
	}

	info(fmt.Sprintf("Ensuring namespace %s and ServiceAccount %s/%s with cluster-admin...", opts.InstallNamespace, opts.InstallNamespace, opts.ServiceAccountName()))

	info("Deleting Subscription and CSV (orphan cascade)...")
	if err := m.PrepareForMigration(ctx, opts, csv); err != nil {
		fail("Preparation failed, attempting recovery...")
		startProgress()
		if recoverErr := m.RecoverFromBackup(ctx, opts, backup); recoverErr != nil {
			clearProgress()
			fail(fmt.Sprintf("Recovery also failed: %v", recoverErr))
			return fmt.Errorf("preparation failed: %w; recovery also failed: %v", err, recoverErr)
		}
		clearProgress()
		warn("Recovered successfully — Subscription restored")
		return fmt.Errorf("preparation failed (recovered successfully): %w", err)
	}
	success("Installer ServiceAccount ready")
	success("OLMv0 management removed (operator workloads running)")

	// Step 6: Create CER (RFC Step 6)
	stepHeader(6, "Creating ClusterExtensionRevision")
	info(fmt.Sprintf("Applying CER %s-1 with %d objects across %d phases...",
		opts.ClusterExtensionName, len(bundleInfo.CollectedObjects), len(kindCounts)))
	info("Waiting for CER to reach Available=True...")
	startProgress()
	if err := m.CreateClusterExtensionRevision(ctx, opts, bundleInfo); err != nil {
		clearProgress()
		fail("CER creation failed, attempting recovery...")
		startProgress()
		if recoverErr := m.RecoverBeforeCE(ctx, opts, backup); recoverErr != nil {
			clearProgress()
			fail(fmt.Sprintf("Recovery also failed: %v", recoverErr))
			return fmt.Errorf("CER creation failed: %w; recovery also failed: %v", err, recoverErr)
		}
		clearProgress()
		warn("Recovered successfully — Subscription restored")
		return fmt.Errorf("CER creation failed (recovered successfully): %w", err)
	}
	clearProgress()
	success(fmt.Sprintf("ClusterExtensionRevision %s-1 is Available", opts.ClusterExtensionName))

	// Step 7: Create CE (RFC Step 7)
	stepHeader(7, "Creating ClusterExtension")
	if bundleInfo.ManualApproval {
		info(fmt.Sprintf("Creating CE %s (package: %s, version: %s [pinned], catalog: %s)...",
			opts.ClusterExtensionName, bundleInfo.PackageName, bundleInfo.Version, bundleInfo.ResolvedCatalogName))
	} else {
		info(fmt.Sprintf("Creating CE %s (package: %s, channel: %s [auto-upgrade], catalog: %s)...",
			opts.ClusterExtensionName, bundleInfo.PackageName, valueOrDefault(bundleInfo.Channel, "default"), bundleInfo.ResolvedCatalogName))
	}
	info("Waiting for CE to reach Installed=True...")
	startProgress()
	if err := m.CreateClusterExtension(ctx, opts, bundleInfo); err != nil {
		clearProgress()
		fail(fmt.Sprintf("Failed to create ClusterExtension: %v", err))
		return fmt.Errorf("failed to create ClusterExtension: %w", err)
	}
	clearProgress()
	success(fmt.Sprintf("ClusterExtension %s is Installed", opts.ClusterExtensionName))

	// Step 8: Cleanup (RFC Step 8)
	stepHeader(8, "Cleaning up OLMv0 resources")
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

	// Notify about CRD-owned ClusterRoles
	crdRoles := m.FindCRDClusterRoles(ctx, csv.Name)
	if len(crdRoles) > 0 {
		info("CRD-owned ClusterRoles retained for RBAC (not managed by OLMv1):")
		for _, name := range crdRoles {
			fmt.Printf("    %s📌 %s%s\n", colorDim, name, colorReset)
		}
	}

	banner(fmt.Sprintf("Migration complete! %s is now managed by OLMv1", bundleInfo.PackageName))

	if backup.Dir != "" {
		info(fmt.Sprintf("Backup files: %s", backup.Dir))
		info("  subscription.yaml — original Subscription")
		info("  clusterserviceversion.yaml — original CSV")
		info("These can be used for manual recovery if needed. Safe to delete once migration is verified.")
	}
	fmt.Println()
	return nil
}

func valueOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func promptCreateCatalog(ctx context.Context, m *migration.Migrator, bundleInfo *migration.MigrationInfo, restConfig *rest.Config) (string, error) {
	catalogName := fmt.Sprintf("%s-catalog", bundleInfo.PackageName)
	imageRef := bundleInfo.CatalogSourceImage

	fmt.Printf("\n  %sThe package was not found in any existing ClusterCatalog.%s\n", colorYellow, colorReset)
	fmt.Printf("  A new ClusterCatalog can be created from the CatalogSource image:\n")
	detail("Name:", catalogName)
	detail("Image:", imageRef)

	if !autoApprove {
		fmt.Printf("\n  %sCreate ClusterCatalog %s? [y/N]: %s", colorYellow, catalogName, colorReset)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			return "", fmt.Errorf("no ClusterCatalog available and user declined to create one")
		}
	}

	info(fmt.Sprintf("Creating ClusterCatalog %s...", catalogName))
	startProgress()
	if err := m.CreateClusterCatalog(ctx, catalogName, imageRef); err != nil {
		clearProgress()
		fail(fmt.Sprintf("Failed to create ClusterCatalog: %v", err))
		return "", fmt.Errorf("failed to create ClusterCatalog: %w", err)
	}
	clearProgress()
	success(fmt.Sprintf("ClusterCatalog %s is serving", catalogName))

	// Verify the package is now available
	info("Verifying package is available in the new catalog...")
	startProgress()
	verifiedName, err := m.ResolveClusterCatalog(ctx, bundleInfo, restConfig)
	clearProgress()
	if err != nil {
		fail(fmt.Sprintf("Package still not found after creating catalog: %v", err))
		return "", fmt.Errorf("package not found in newly created ClusterCatalog: %w", err)
	}

	return verifiedName, nil
}
