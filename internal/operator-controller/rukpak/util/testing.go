package util

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

type CSVOption func(version *v1alpha1.ClusterServiceVersion)

//nolint:unparam
func WithName(name string) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Name = name
	}
}

func WithStrategyDeploymentSpecs(strategyDeploymentSpecs ...v1alpha1.StrategyDeploymentSpec) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs = strategyDeploymentSpecs
	}
}

func WithAnnotations(annotations map[string]string) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Annotations = annotations
	}
}

func WithPermissions(permissions ...v1alpha1.StrategyDeploymentPermissions) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.InstallStrategy.StrategySpec.Permissions = permissions
	}
}

func WithClusterPermissions(permissions ...v1alpha1.StrategyDeploymentPermissions) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions = permissions
	}
}

func WithOwnedCRDs(crdDesc ...v1alpha1.CRDDescription) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.CustomResourceDefinitions.Owned = crdDesc
	}
}

func MakeCSV(opts ...CSVOption) v1alpha1.ClusterServiceVersion {
	csv := v1alpha1.ClusterServiceVersion{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.SchemeGroupVersion.String(),
			Kind:       "ClusterServiceVersion",
		},
	}
	for _, opt := range opts {
		opt(&csv)
	}
	return csv
}
