package migration

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

// CompatibilityIssue describes a single compatibility problem that prevents migration.
type CompatibilityIssue struct {
	Check   string
	Message string
}

func (c CompatibilityIssue) String() string {
	return fmt.Sprintf("[%s] %s", c.Check, c.Message)
}

// CheckCompatibility runs all compatibility checks and returns a report with individual results.
func (m *Migrator) CheckCompatibility(ctx context.Context, opts Options, csv *operatorsv1alpha1.ClusterServiceVersion, bundleProperties string) (*PreMigrationReport, error) {
	report := &PreMigrationReport{}

	// OperatorGroup checks
	ogChecks, err := m.checkAllNamespacesMode(ctx, opts)
	if err != nil {
		return nil, err
	}
	report.Checks = append(report.Checks, ogChecks...)

	// Dependency checks
	report.Checks = append(report.Checks, checkNoDependencies(bundleProperties)...)

	// APIService checks
	report.Checks = append(report.Checks, checkNoAPIServices(csv))

	// OperatorCondition checks
	condCheck, err := m.checkNoOperatorConditions(ctx, opts, csv)
	if err != nil {
		return nil, err
	}
	report.Checks = append(report.Checks, condCheck)

	return report, nil
}

func (m *Migrator) checkAllNamespacesMode(ctx context.Context, opts Options) ([]CheckResult, error) {
	var ogList operatorsv1.OperatorGroupList
	if err := m.Client.List(ctx, &ogList, client.InNamespace(opts.SubscriptionNamespace)); err != nil {
		return nil, fmt.Errorf("failed to list OperatorGroups in %s: %w", opts.SubscriptionNamespace, err)
	}
	if len(ogList.Items) == 0 {
		return []CheckResult{{
			Name:    "OperatorGroup exists",
			Passed:  false,
			Message: fmt.Sprintf("no OperatorGroup found in namespace %s", opts.SubscriptionNamespace),
		}}, nil
	}

	og := ogList.Items[0]
	var checks []CheckResult

	// spec.serviceAccountName
	if og.Spec.ServiceAccountName != "" {
		checks = append(checks, CheckResult{
			Name:    "No scoped ServiceAccount",
			Passed:  false,
			Message: "OperatorGroup has spec.serviceAccountName set; OLMv1 does not support scoped service accounts",
		})
	} else {
		checks = append(checks, CheckResult{
			Name:    "No scoped ServiceAccount",
			Passed:  true,
			Message: "OperatorGroup does not use a scoped service account",
		})
	}

	// spec.selector
	if og.Spec.Selector != nil && !isEmptyLabelSelector(og.Spec.Selector) {
		checks = append(checks, CheckResult{
			Name:    "No namespace selector",
			Passed:  false,
			Message: "OperatorGroup has spec.selector set; must convert to spec.targetNamespaces before migration",
		})
	} else {
		checks = append(checks, CheckResult{
			Name:    "No namespace selector",
			Passed:  true,
			Message: "OperatorGroup does not use a namespace selector",
		})
	}

	// spec.upgradeStrategy
	if og.Spec.UpgradeStrategy != "" && og.Spec.UpgradeStrategy != operatorsv1.UpgradeStrategyDefault {
		checks = append(checks, CheckResult{
			Name:    "Upgrade strategy",
			Passed:  false,
			Message: fmt.Sprintf("must be %q or unset, got %q", operatorsv1.UpgradeStrategyDefault, og.Spec.UpgradeStrategy),
		})
	} else {
		checks = append(checks, CheckResult{
			Name:    "Upgrade strategy",
			Passed:  true,
			Message: "upgrade strategy is Default or unset",
		})
	}

	// spec.targetNamespaces (AllNamespaces mode)
	if len(og.Spec.TargetNamespaces) > 0 {
		checks = append(checks, CheckResult{
			Name:    "AllNamespaces mode",
			Passed:  false,
			Message: "OperatorGroup has spec.targetNamespaces set; operator must be in AllNamespaces mode",
		})
	} else {
		checks = append(checks, CheckResult{
			Name:    "AllNamespaces mode",
			Passed:  true,
			Message: "operator is in AllNamespaces mode",
		})
	}

	// status.namespaces warning
	if len(og.Status.Namespaces) == 1 && og.Status.Namespaces[0] != "" {
		checks = append(checks, CheckResult{
			Name:    "Namespace scope change",
			Passed:  false,
			Message: fmt.Sprintf("OperatorGroup targets namespace %q; post-migration the operator will run in AllNamespaces mode", og.Status.Namespaces[0]),
		})
	}

	return checks, nil
}

func isEmptyLabelSelector(s *metav1.LabelSelector) bool {
	return s == nil || (len(s.MatchLabels) == 0 && len(s.MatchExpressions) == 0)
}

// olmProperty represents a single entry in the operatorframework.io/properties annotation.
type olmProperty struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// parseProperties handles both formats of the operatorframework.io/properties annotation:
//   - bare array: [{"type":"olm.package","value":{...}}, ...]
//   - wrapped object: {"properties": [{"type":"olm.package","value":{...}}, ...]}
func parseProperties(propertiesJSON string) ([]olmProperty, error) {
	raw := []byte(propertiesJSON)

	// Try bare array first
	var props []olmProperty
	if err := json.Unmarshal(raw, &props); err == nil {
		return props, nil
	}

	// Try wrapped object
	var wrapped struct {
		Properties []olmProperty `json:"properties"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Properties, nil
}

func checkNoDependencies(propertiesJSON string) []CheckResult {
	if propertiesJSON == "" {
		return []CheckResult{{
			Name:    "No dependency resolution",
			Passed:  true,
			Message: "no bundle properties declared",
		}}
	}

	props, err := parseProperties(propertiesJSON)
	if err != nil {
		return []CheckResult{{
			Name:    "No dependency resolution",
			Passed:  false,
			Message: fmt.Sprintf("failed to parse bundle properties: %v", err),
		}}
	}

	var issues []CheckResult
	for _, p := range props {
		switch p.Type {
		case "olm.package.required":
			issues = append(issues, CheckResult{
				Name:    "No dependency resolution",
				Passed:  false,
				Message: fmt.Sprintf("bundle declares olm.package.required dependency: %s", string(p.Value)),
			})
		case "olm.gvk.required":
			issues = append(issues, CheckResult{
				Name:    "No dependency resolution",
				Passed:  false,
				Message: fmt.Sprintf("bundle declares olm.gvk.required dependency: %s", string(p.Value)),
			})
		}
	}

	if len(issues) == 0 {
		return []CheckResult{{
			Name:    "No dependency resolution",
			Passed:  true,
			Message: "no olm.package.required or olm.gvk.required properties",
		}}
	}
	return issues
}

func checkNoAPIServices(csv *operatorsv1alpha1.ClusterServiceVersion) CheckResult {
	if csv.Spec.APIServiceDefinitions.Owned != nil || csv.Spec.APIServiceDefinitions.Required != nil {
		return CheckResult{
			Name:    "No APIService definitions",
			Passed:  false,
			Message: "CSV has spec.apiservicedefinitions set; OLMv1 does not support APIService definitions",
		}
	}
	return CheckResult{
		Name:    "No APIService definitions",
		Passed:  true,
		Message: "CSV does not define APIServices",
	}
}

func (m *Migrator) checkNoOperatorConditions(ctx context.Context, opts Options, csv *operatorsv1alpha1.ClusterServiceVersion) (CheckResult, error) {
	var oc operatorsv1.OperatorCondition
	err := m.Client.Get(ctx, types.NamespacedName{
		Name:      csv.Name,
		Namespace: opts.SubscriptionNamespace,
	}, &oc)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return CheckResult{}, fmt.Errorf("failed to get OperatorCondition: %w", err)
		}
		return CheckResult{
			Name:    "No OperatorCondition usage",
			Passed:  true,
			Message: "no OperatorCondition resource found",
		}, nil
	}

	if len(oc.Status.Conditions) > 0 {
		return CheckResult{
			Name:    "No OperatorCondition usage",
			Passed:  false,
			Message: "OperatorCondition has status.conditions entries; operator actively uses the OperatorCondition API",
		}, nil
	}
	return CheckResult{
		Name:    "No OperatorCondition usage",
		Passed:  true,
		Message: "OperatorCondition exists but has no status entries",
	}, nil
}
