package clusterserviceversion_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

func Test_Builder(t *testing.T) {
	t.Run("builds an empty csv by default", func(t *testing.T) {
		obj := clusterserviceversion.Builder().Build()
		require.Equal(t, v1alpha1.ClusterServiceVersion{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterServiceVersion",
				APIVersion: v1alpha1.SchemeGroupVersion.String(),
			},
		}, obj)
	})

	t.Run("WithName sets csv .metadata.name", func(t *testing.T) {
		obj := clusterserviceversion.Builder().WithName("some-name").Build()
		require.Equal(t, v1alpha1.ClusterServiceVersion{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterServiceVersion",
				APIVersion: v1alpha1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-name",
			},
		}, obj)
	})

	t.Run("WithStrategyDeploymentSpecs sets csv .spec.install.spec.deployments", func(t *testing.T) {
		obj := clusterserviceversion.Builder().WithStrategyDeploymentSpecs(
			v1alpha1.StrategyDeploymentSpec{
				Name: "spec-one",
			},
			v1alpha1.StrategyDeploymentSpec{
				Name: "spec-two",
			},
		).Build()

		require.Equal(t, v1alpha1.ClusterServiceVersion{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterServiceVersion",
				APIVersion: v1alpha1.SchemeGroupVersion.String(),
			},
			Spec: v1alpha1.ClusterServiceVersionSpec{
				InstallStrategy: v1alpha1.NamedInstallStrategy{
					StrategySpec: v1alpha1.StrategyDetailsDeployment{
						DeploymentSpecs: []v1alpha1.StrategyDeploymentSpec{
							{
								Name: "spec-one",
							},
							{
								Name: "spec-two",
							},
						},
					},
				},
			},
		}, obj)
	})

	t.Run("WithPermissions sets csv .spec.install.spec.permissions", func(t *testing.T) {
		obj := clusterserviceversion.Builder().WithPermissions(
			v1alpha1.StrategyDeploymentPermissions{
				ServiceAccountName: "service-account",
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"secrets"},
						Verbs:     []string{"list", "watch"},
					},
				},
			},
			v1alpha1.StrategyDeploymentPermissions{
				ServiceAccountName: "",
			},
		).Build()

		require.Equal(t, v1alpha1.ClusterServiceVersion{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterServiceVersion",
				APIVersion: v1alpha1.SchemeGroupVersion.String(),
			},
			Spec: v1alpha1.ClusterServiceVersionSpec{
				InstallStrategy: v1alpha1.NamedInstallStrategy{
					StrategySpec: v1alpha1.StrategyDetailsDeployment{
						Permissions: []v1alpha1.StrategyDeploymentPermissions{
							{
								ServiceAccountName: "service-account",
								Rules: []rbacv1.PolicyRule{
									{
										APIGroups: []string{""},
										Resources: []string{"secrets"},
										Verbs:     []string{"list", "watch"},
									},
								},
							},
							{
								ServiceAccountName: "",
							},
						},
					},
				},
			},
		}, obj)
	})

	t.Run("WithClusterPermissions sets csv .spec.install.spec.clusterPermissions", func(t *testing.T) {
		obj := clusterserviceversion.Builder().WithClusterPermissions(
			v1alpha1.StrategyDeploymentPermissions{
				ServiceAccountName: "service-account",
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"secrets"},
						Verbs:     []string{"list", "watch"},
					},
				},
			},
			v1alpha1.StrategyDeploymentPermissions{
				ServiceAccountName: "",
			},
		).Build()

		require.Equal(t, v1alpha1.ClusterServiceVersion{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterServiceVersion",
				APIVersion: v1alpha1.SchemeGroupVersion.String(),
			},
			Spec: v1alpha1.ClusterServiceVersionSpec{
				InstallStrategy: v1alpha1.NamedInstallStrategy{
					StrategySpec: v1alpha1.StrategyDetailsDeployment{
						ClusterPermissions: []v1alpha1.StrategyDeploymentPermissions{
							{
								ServiceAccountName: "service-account",
								Rules: []rbacv1.PolicyRule{
									{
										APIGroups: []string{""},
										Resources: []string{"secrets"},
										Verbs:     []string{"list", "watch"},
									},
								},
							},
							{
								ServiceAccountName: "",
							},
						},
					},
				},
			},
		}, obj)
	})

	t.Run("WithClusterPermissions sets csv .spec.customresourcedefinitions.owned", func(t *testing.T) {
		obj := clusterserviceversion.Builder().WithOwnedCRDs(
			v1alpha1.CRDDescription{Name: "a.crd.something"},
			v1alpha1.CRDDescription{Name: "b.crd.something"},
		).Build()

		require.Equal(t, v1alpha1.ClusterServiceVersion{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterServiceVersion",
				APIVersion: v1alpha1.SchemeGroupVersion.String(),
			},
			Spec: v1alpha1.ClusterServiceVersionSpec{
				CustomResourceDefinitions: v1alpha1.CustomResourceDefinitions{
					Owned: []v1alpha1.CRDDescription{
						{Name: "a.crd.something"},
						{Name: "b.crd.something"},
					},
				},
			},
		}, obj)
	})

	t.Run("WithInstallModeSupportFor adds all install modes to .spec.installModes and sets supported to true for the given supported install modes", func(t *testing.T) {
		obj := clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace).Build()

		require.Equal(t, v1alpha1.ClusterServiceVersion{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterServiceVersion",
				APIVersion: v1alpha1.SchemeGroupVersion.String(),
			},
			Spec: v1alpha1.ClusterServiceVersionSpec{
				InstallModes: []v1alpha1.InstallMode{
					{
						Type:      v1alpha1.InstallModeTypeAllNamespaces,
						Supported: true,
					},
					{
						Type:      v1alpha1.InstallModeTypeSingleNamespace,
						Supported: true,
					},
					{
						Type:      v1alpha1.InstallModeTypeMultiNamespace,
						Supported: false,
					},
					{
						Type:      v1alpha1.InstallModeTypeOwnNamespace,
						Supported: false,
					},
				},
			},
		}, obj)
	})
}
