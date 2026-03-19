package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/operator-framework/operator-controller/internal/operator-controller/migration"
)

var gatherCmd = &cobra.Command{
	Use:   "gather",
	Short: "Gather migration info without creating resources",
	Long: `Profiles the operator installation and collects all resources that would be
migrated, without actually creating any OLMv1 resources. Useful for reviewing
what the migration will do before committing.

Examples:
  migrate gather -s my-operator -n operators`,
	RunE: runGather,
}

func runGather(cmd *cobra.Command, _ []string) error {
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

	fmt.Printf("\n%s%s📋 Gathering migration info for %s/%s%s\n", colorBold, colorCyan, subscriptionNamespace, subscriptionName, colorReset)

	// Profile
	sectionHeader("Operator Profile")
	info("Reading Subscription, CSV, and InstallPlan...")
	sub, csv, ip, err := m.GetCSVAndInstallPlan(ctx, opts)
	if err != nil {
		fail(fmt.Sprintf("Failed to profile operator: %v", err))
		return fmt.Errorf("failed to profile operator: %w", err)
	}

	bundleInfo, err := m.GetBundleInfo(ctx, opts, csv, ip)
	if err != nil {
		fail(fmt.Sprintf("Failed to get bundle info: %v", err))
		return fmt.Errorf("failed to get bundle info: %w", err)
	}

	detail("Package:", bundleInfo.PackageName)
	detail("Version:", bundleInfo.Version)
	detail("Bundle:", bundleInfo.BundleName)
	detail("Channel:", valueOrDefault(bundleInfo.Channel, "(default)"))
	detail("Bundle Image:", valueOrDefault(bundleInfo.BundleImage, "(not available)"))
	detail("CatalogSource:", fmt.Sprintf("%s/%s", bundleInfo.CatalogSourceRef.Namespace, bundleInfo.CatalogSourceRef.Name))
	detail("Sub State:", fmt.Sprintf("%s (installed: %s)", sub.Status.State, sub.Status.InstalledCSV))

	// Catalog source image
	csImage, err := m.GetCatalogSourceImage(ctx, bundleInfo.CatalogSourceRef)
	if err != nil {
		detail("Catalog Image:", fmt.Sprintf("%s(unavailable)%s", colorYellow, colorReset))
	} else {
		detail("Catalog Image:", csImage)
		bundleInfo.CatalogSourceImage = csImage
	}

	// Resolve ClusterCatalog
	catalogName, err := m.ResolveClusterCatalog(ctx, bundleInfo, restConfig)
	if err != nil {
		detail("ClusterCatalog:", fmt.Sprintf("%s(not resolved: %v)%s", colorYellow, err, colorReset))
	} else {
		detail("ClusterCatalog:", catalogName)
	}

	// CSV status
	sectionHeader("CSV Status")
	detail("Name:", csv.Name)
	detail("Phase:", string(csv.Status.Phase))
	detail("Reason:", string(csv.Status.Reason))

	// Collect resources
	sectionHeader("Collected Resources")
	info("Scanning Operator CR, owner labels, ownerRefs, and InstallPlan steps...")
	objects, err := m.CollectResources(ctx, opts, csv, ip, bundleInfo.PackageName)
	if err != nil {
		fail(fmt.Sprintf("Failed to collect resources: %v", err))
		return fmt.Errorf("failed to collect resources: %w", err)
	}

	// Group by kind for display
	kindCounts := make(map[string]int)
	for _, obj := range objects {
		kindCounts[obj.GetKind()]++
	}

	success(fmt.Sprintf("Found %d resources across %d kinds", len(objects), len(kindCounts)))
	fmt.Println()
	for kind, count := range kindCounts {
		fmt.Printf("  %s%-40s%s %s%d%s\n", colorDim, kind, colorReset, colorBold, count, colorReset)
	}

	fmt.Println()
	for _, obj := range objects {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = "(cluster)"
		}
		resource(obj.GetKind(), ns, obj.GetName())
	}

	// Migration plan
	sectionHeader("Migration Plan")
	detail("CE name:", opts.ClusterExtensionName)
	detail("Install NS:", opts.InstallNamespace)
	detail("ServiceAccount:", opts.ServiceAccountName())
	detail("CER name:", fmt.Sprintf("%s-1", opts.ClusterExtensionName))
	detail("Collision:", "None (adopt existing objects)")

	fmt.Println()
	return nil
}
