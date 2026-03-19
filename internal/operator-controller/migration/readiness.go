package migration

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

// CheckReadiness verifies that the cluster is ready for migration and returns
// individual check results. It checks Subscription state, CSV health, uniqueness,
// and dependency status.
//
// OLMv0 does NOT need to be scaled down. The migration safely coexists with running
// OLMv0 controllers because:
//   - The Subscription is deleted first, stopping OLMv0 from reconciling this operator
//   - All deletions use orphan cascading, preserving operator workloads
func (m *Migrator) CheckReadiness(ctx context.Context, opts Options) (*PreMigrationReport, error) {
	report := &PreMigrationReport{}

	var sub operatorsv1alpha1.Subscription
	if err := m.Client.Get(ctx, types.NamespacedName{
		Name:      opts.SubscriptionName,
		Namespace: opts.SubscriptionNamespace,
	}, &sub); err != nil {
		return nil, fmt.Errorf("failed to get Subscription %s/%s: %w", opts.SubscriptionNamespace, opts.SubscriptionName, err)
	}

	// Subscription state
	if sub.Status.State == operatorsv1alpha1.SubscriptionStateAtLatest ||
		sub.Status.State == operatorsv1alpha1.SubscriptionStateUpgradePending {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "Subscription state",
			Passed:  true,
			Message: fmt.Sprintf("state is %q", sub.Status.State),
		})
	} else {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "Subscription state",
			Passed:  false,
			Message: fmt.Sprintf("must be %q or %q, got %q", operatorsv1alpha1.SubscriptionStateAtLatest, operatorsv1alpha1.SubscriptionStateUpgradePending, sub.Status.State),
		})
	}

	// installedCSV
	if sub.Status.InstalledCSV != "" {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "Installed CSV",
			Passed:  true,
			Message: sub.Status.InstalledCSV,
		})
	} else {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "Installed CSV",
			Passed:  false,
			Message: "no installedCSV set",
		})
	}

	// olm.generated-by
	if _, ok := sub.Annotations["olm.generated-by"]; ok {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "Not a dependency",
			Passed:  false,
			Message: "olm.generated-by annotation present — operator is a dependency of another",
		})
	} else {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "Not a dependency",
			Passed:  true,
			Message: "no olm.generated-by annotation",
		})
	}

	// Uniqueness
	var subList operatorsv1alpha1.SubscriptionList
	if err := m.Client.List(ctx, &subList); err != nil {
		return nil, fmt.Errorf("failed to list Subscriptions: %w", err)
	}
	duplicate := false
	for _, other := range subList.Items {
		if other.Name == sub.Name && other.Namespace == sub.Namespace {
			continue
		}
		if other.Spec.Package == sub.Spec.Package {
			report.Checks = append(report.Checks, CheckResult{
				Name:    "Package uniqueness",
				Passed:  false,
				Message: fmt.Sprintf("another Subscription %s/%s references the same package %q", other.Namespace, other.Name, sub.Spec.Package),
			})
			duplicate = true
			break
		}
	}
	if !duplicate {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "Package uniqueness",
			Passed:  true,
			Message: fmt.Sprintf("no other Subscription references package %q", sub.Spec.Package),
		})
	}

	// CSV phase and reason
	if sub.Status.InstalledCSV != "" {
		csvName := sub.Status.InstalledCSV
		var csv operatorsv1alpha1.ClusterServiceVersion
		if err := m.Client.Get(ctx, types.NamespacedName{
			Name:      csvName,
			Namespace: opts.SubscriptionNamespace,
		}, &csv); err != nil {
			report.Checks = append(report.Checks, CheckResult{
				Name:    "CSV health",
				Passed:  false,
				Message: fmt.Sprintf("failed to get CSV %s: %v", csvName, err),
			})
		} else if csv.Status.Phase != operatorsv1alpha1.CSVPhaseSucceeded {
			report.Checks = append(report.Checks, CheckResult{
				Name:    "CSV health",
				Passed:  false,
				Message: fmt.Sprintf("phase is %q, expected %q", csv.Status.Phase, operatorsv1alpha1.CSVPhaseSucceeded),
			})
		} else if csv.Status.Reason != operatorsv1alpha1.CSVReasonInstallSuccessful {
			report.Checks = append(report.Checks, CheckResult{
				Name:    "CSV health",
				Passed:  false,
				Message: fmt.Sprintf("reason is %q, expected %q", csv.Status.Reason, operatorsv1alpha1.CSVReasonInstallSuccessful),
			})
		} else {
			report.Checks = append(report.Checks, CheckResult{
				Name:    "CSV health",
				Passed:  true,
				Message: fmt.Sprintf("phase: %s, reason: %s", csv.Status.Phase, csv.Status.Reason),
			})
		}
	}

	return report, nil
}
