package migration

import (
	"context"
	"fmt"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

// OperatorScanResult holds the result of scanning a single Subscription for migration eligibility.
type OperatorScanResult struct {
	SubscriptionName      string
	SubscriptionNamespace string
	PackageName           string
	InstalledCSV          string
	Version               string
	State                 string
	Eligible              bool
	Error                 error
	FailedChecks          []CheckResult
}

// ScanAllSubscriptions discovers all Subscriptions on the cluster and checks each
// for migration eligibility (readiness + compatibility).
func (m *Migrator) ScanAllSubscriptions(ctx context.Context) ([]OperatorScanResult, error) {
	var subList operatorsv1alpha1.SubscriptionList
	if err := m.Client.List(ctx, &subList); err != nil {
		return nil, fmt.Errorf("failed to list Subscriptions: %w", err)
	}

	var results []OperatorScanResult
	for _, sub := range subList.Items {
		result := OperatorScanResult{
			SubscriptionName:      sub.Name,
			SubscriptionNamespace: sub.Namespace,
			PackageName:           sub.Spec.Package,
			InstalledCSV:          sub.Status.InstalledCSV,
			State:                 string(sub.Status.State),
		}

		opts := Options{
			SubscriptionName:      sub.Name,
			SubscriptionNamespace: sub.Namespace,
		}
		opts.ApplyDefaults()

		m.progress(fmt.Sprintf("Checking %s/%s (%s)...", sub.Namespace, sub.Name, sub.Spec.Package))

		// Check readiness
		readiness, err := m.CheckReadiness(ctx, opts)
		if err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}

		// Get CSV for compatibility checks
		_, csv, _, err := m.GetCSVAndInstallPlan(ctx, opts)
		if err != nil {
			result.Error = fmt.Errorf("failed to get CSV: %w", err)
			results = append(results, result)
			continue
		}

		result.Version = parseCSVVersion(csv)

		// Check compatibility
		propsJSON := csv.Annotations["operatorframework.io/properties"]
		compat, err := m.CheckCompatibility(ctx, opts, csv, propsJSON)
		if err != nil {
			result.Error = fmt.Errorf("compatibility check error: %w", err)
			results = append(results, result)
			continue
		}

		// Merge failed checks from both reports
		result.FailedChecks = append(readiness.FailedChecks(), compat.FailedChecks()...)
		result.Eligible = len(result.FailedChecks) == 0
		results = append(results, result)
	}

	return results, nil
}
