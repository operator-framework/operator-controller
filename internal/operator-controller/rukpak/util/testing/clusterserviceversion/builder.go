package clusterserviceversion

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

var installModes = []v1alpha1.InstallModeType{
	v1alpha1.InstallModeTypeAllNamespaces,
	v1alpha1.InstallModeTypeSingleNamespace,
	v1alpha1.InstallModeTypeMultiNamespace,
	v1alpha1.InstallModeTypeOwnNamespace,
}

// ClusterServiceVersionBuilder build a ClusterServiceVersion resource
type ClusterServiceVersionBuilder interface {
	WithName(name string) ClusterServiceVersionBuilder
	WithStrategyDeploymentSpecs(strategyDeploymentSpecs ...v1alpha1.StrategyDeploymentSpec) ClusterServiceVersionBuilder
	WithAnnotations(annotations map[string]string) ClusterServiceVersionBuilder
	WithPermissions(permissions ...v1alpha1.StrategyDeploymentPermissions) ClusterServiceVersionBuilder
	WithClusterPermissions(permissions ...v1alpha1.StrategyDeploymentPermissions) ClusterServiceVersionBuilder
	WithOwnedCRDs(crdDesc ...v1alpha1.CRDDescription) ClusterServiceVersionBuilder
	WithInstallModeSupportFor(installModeType ...v1alpha1.InstallModeType) ClusterServiceVersionBuilder
	WithWebhookDefinitions(webhookDefinitions ...v1alpha1.WebhookDescription) ClusterServiceVersionBuilder
	WithOwnedAPIServiceDescriptions(ownedAPIServiceDescriptions ...v1alpha1.APIServiceDescription) ClusterServiceVersionBuilder
	Build() v1alpha1.ClusterServiceVersion
}

// Builder creates a new ClusterServiceVersionBuilder for building ClusterServiceVersion resources
func Builder() ClusterServiceVersionBuilder {
	return &clusterServiceVersionBuilder{
		csv: v1alpha1.ClusterServiceVersion{
			TypeMeta: metav1.TypeMeta{
				APIVersion: v1alpha1.SchemeGroupVersion.String(),
				Kind:       "ClusterServiceVersion",
			},
		},
	}
}

type clusterServiceVersionBuilder struct {
	csv v1alpha1.ClusterServiceVersion
}

//nolint:unparam
func (b *clusterServiceVersionBuilder) WithName(name string) ClusterServiceVersionBuilder {
	b.csv.Name = name
	return b
}

func (b *clusterServiceVersionBuilder) WithStrategyDeploymentSpecs(strategyDeploymentSpecs ...v1alpha1.StrategyDeploymentSpec) ClusterServiceVersionBuilder {
	b.csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs = strategyDeploymentSpecs
	return b
}

func (b *clusterServiceVersionBuilder) WithAnnotations(annotations map[string]string) ClusterServiceVersionBuilder {
	b.csv.Annotations = annotations
	return b
}

func (b *clusterServiceVersionBuilder) WithPermissions(permissions ...v1alpha1.StrategyDeploymentPermissions) ClusterServiceVersionBuilder {
	b.csv.Spec.InstallStrategy.StrategySpec.Permissions = permissions
	return b
}

func (b *clusterServiceVersionBuilder) WithClusterPermissions(permissions ...v1alpha1.StrategyDeploymentPermissions) ClusterServiceVersionBuilder {
	b.csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions = permissions
	return b
}

func (b *clusterServiceVersionBuilder) WithOwnedCRDs(crdDesc ...v1alpha1.CRDDescription) ClusterServiceVersionBuilder {
	b.csv.Spec.CustomResourceDefinitions.Owned = crdDesc
	return b
}

func (b *clusterServiceVersionBuilder) WithInstallModeSupportFor(installModeType ...v1alpha1.InstallModeType) ClusterServiceVersionBuilder {
	supportedInstallModes := sets.New(installModeType...)
	csvInstallModes := make([]v1alpha1.InstallMode, 0, len(installModeType))
	for _, t := range installModes {
		csvInstallModes = append(csvInstallModes, v1alpha1.InstallMode{
			Type:      t,
			Supported: supportedInstallModes.Has(t),
		})
	}
	b.csv.Spec.InstallModes = csvInstallModes
	return b
}

func (b *clusterServiceVersionBuilder) WithWebhookDefinitions(webhookDefinitions ...v1alpha1.WebhookDescription) ClusterServiceVersionBuilder {
	b.csv.Spec.WebhookDefinitions = webhookDefinitions
	return b
}

func (b *clusterServiceVersionBuilder) WithOwnedAPIServiceDescriptions(ownedAPIServiceDescriptions ...v1alpha1.APIServiceDescription) ClusterServiceVersionBuilder {
	b.csv.Spec.APIServiceDefinitions.Owned = ownedAPIServiceDescriptions
	return b
}

func (b *clusterServiceVersionBuilder) Build() v1alpha1.ClusterServiceVersion {
	return b.csv
}
