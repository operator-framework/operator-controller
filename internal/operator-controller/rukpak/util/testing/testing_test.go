package testing_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
)

func Test_MakeCSV(t *testing.T) {
	csv := MakeCSV()
	require.Equal(t, v1alpha1.ClusterServiceVersion{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterServiceVersion",
			APIVersion: v1alpha1.SchemeGroupVersion.String(),
		},
	}, csv)
}

func Test_MakeCSV_WithName(t *testing.T) {
	csv := MakeCSV(WithName("some-name"))
	require.Equal(t, v1alpha1.ClusterServiceVersion{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterServiceVersion",
			APIVersion: v1alpha1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "some-name",
		},
	}, csv)
}

func Test_MakeCSV_WithStrategyDeploymentSpecs(t *testing.T) {
	csv := MakeCSV(
		WithStrategyDeploymentSpecs(
			v1alpha1.StrategyDeploymentSpec{
				Name: "spec-one",
			},
			v1alpha1.StrategyDeploymentSpec{
				Name: "spec-two",
			},
		),
	)

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
	}, csv)
}

func Test_MakeCSV_WithPermissions(t *testing.T) {
	csv := MakeCSV(
		WithPermissions(
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
		),
	)

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
	}, csv)
}

func Test_MakeCSV_WithClusterPermissions(t *testing.T) {
	csv := MakeCSV(
		WithClusterPermissions(
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
		),
	)

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
	}, csv)
}

func Test_MakeCSV_WithOwnedCRDs(t *testing.T) {
	csv := MakeCSV(
		WithOwnedCRDs(
			v1alpha1.CRDDescription{Name: "a.crd.something"},
			v1alpha1.CRDDescription{Name: "b.crd.something"},
		),
	)

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
	}, csv)
}

func Test_MakeCSV_WithInstallModeSupportFor(t *testing.T) {
	csv := MakeCSV(
		WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace),
	)

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
	}, csv)
}
