package render_test

import (
	"cmp"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert/render"
)

func Test_BundleDeploymentGenerator(t *testing.T) {
	for _, tc := range []struct {
		name              string
		bundle            *convert.RegistryV1
		opts              render.Options
		expectedResources []client.Object
	}{
		{
			name: "generates deployment resources",
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithAnnotations(map[string]string{
						"csv": "annotation",
					}),
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "deployment-one",
							Label: map[string]string{
								"bar": "foo",
							},
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									ObjectMeta: metav1.ObjectMeta{
										Annotations: map[string]string{
											"pod": "annotation",
										},
									},
								},
							},
						},
						v1alpha1.StrategyDeploymentSpec{
							Name: "deployment-two",
							Spec: appsv1.DeploymentSpec{},
						},
					),
				),
			},
			opts: render.Options{
				InstallNamespace: "install-namespace",
				TargetNamespaces: []string{"watch-namespace-one", "watch-namespace-two"},
			},
			expectedResources: []client.Object{
				&appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Deployment",
						APIVersion: appsv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "install-namespace",
						Name:      "deployment-one",
						Labels: map[string]string{
							"bar": "foo",
						},
					},
					Spec: appsv1.DeploymentSpec{
						RevisionHistoryLimit: ptr.To(int32(1)),
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Annotations: map[string]string{
									"csv":                  "annotation",
									"olm.targetNamespaces": "watch-namespace-one,watch-namespace-two",
									"pod":                  "annotation",
								},
							},
						},
					},
				},
				&appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Deployment",
						APIVersion: appsv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "install-namespace",
						Name:      "deployment-two",
					},
					Spec: appsv1.DeploymentSpec{
						RevisionHistoryLimit: ptr.To(int32(1)),
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Annotations: map[string]string{
									"csv":                  "annotation",
									"olm.targetNamespaces": "watch-namespace-one,watch-namespace-two",
								},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objs, err := render.BundleDeploymentGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResources, objs)
		})
	}
}

func Test_BundlePermissionsGenerator(t *testing.T) {
	fakeUniqueNameGenerator := func(base string, _ interface{}) (string, error) {
		return base, nil
	}

	for _, tc := range []struct {
		name              string
		opts              render.Options
		bundle            *convert.RegistryV1
		expectedResources []client.Object
	}{
		{
			name: "does not generate any resources when in AllNamespaces mode (target namespace is [''])",
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{""},
				UniqueNameGenerator: fakeUniqueNameGenerator,
			},
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithName("csv"),
					WithPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-one",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								},
							},
						},
					),
				),
			},
			expectedResources: nil,
		},
		{
			name: "generates role and rolebinding for permission service-account when in Single/OwnNamespace mode (target namespace contains a single namespace)",
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{"watch-namespace"},
				UniqueNameGenerator: fakeUniqueNameGenerator,
			},
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithName("csv"),
					WithPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-one",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								}, {
									APIGroups: []string{"appsv1"},
									Resources: []string{"deployments"},
									Verbs:     []string{"create"},
								},
							},
						},
					),
				),
			},
			expectedResources: []client.Object{
				&rbacv1.Role{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Role",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace",
						Name:      "csv-service-account-one",
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{""},
							Resources: []string{"namespaces"},
							Verbs:     []string{"get", "list", "watch"},
						}, {
							APIGroups: []string{"appsv1"},
							Resources: []string{"deployments"},
							Verbs:     []string{"create"},
						},
					},
				},
				&rbacv1.RoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "RoleBinding",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace",
						Name:      "csv-service-account-one",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							APIGroup:  "",
							Name:      "service-account-one",
							Namespace: "install-namespace",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     "csv-service-account-one",
					},
				},
			},
		},
		{
			name: "generates role and rolebinding for permission service-account for each target namespace when in MultiNamespace install mode (target namespace contains multiple namespaces)",
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{"watch-namespace", "watch-namespace-two"},
				UniqueNameGenerator: fakeUniqueNameGenerator,
			},
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithName("csv"),
					WithPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-one",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								}, {
									APIGroups: []string{"appsv1"},
									Resources: []string{"deployments"},
									Verbs:     []string{"create"},
								},
							},
						},
					),
				),
			},
			expectedResources: []client.Object{
				&rbacv1.Role{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Role",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace",
						Name:      "csv-service-account-one",
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{""},
							Resources: []string{"namespaces"},
							Verbs:     []string{"get", "list", "watch"},
						}, {
							APIGroups: []string{"appsv1"},
							Resources: []string{"deployments"},
							Verbs:     []string{"create"},
						},
					},
				},
				&rbacv1.RoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "RoleBinding",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace",
						Name:      "csv-service-account-one",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							APIGroup:  "",
							Name:      "service-account-one",
							Namespace: "install-namespace",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     "csv-service-account-one",
					},
				},
				&rbacv1.Role{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Role",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace-two",
						Name:      "csv-service-account-one",
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{""},
							Resources: []string{"namespaces"},
							Verbs:     []string{"get", "list", "watch"},
						}, {
							APIGroups: []string{"appsv1"},
							Resources: []string{"deployments"},
							Verbs:     []string{"create"},
						},
					},
				},
				&rbacv1.RoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "RoleBinding",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace-two",
						Name:      "csv-service-account-one",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							APIGroup:  "",
							Name:      "service-account-one",
							Namespace: "install-namespace",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     "csv-service-account-one",
					},
				},
			},
		},
		{
			name: "generates role and rolebinding for each permission service-account",
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{"watch-namespace"},
				UniqueNameGenerator: fakeUniqueNameGenerator,
			},
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithName("csv"),
					WithPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-one",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								},
							},
						},
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-two",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{"appsv1"},
									Resources: []string{"deployments"},
									Verbs:     []string{"create"},
								},
							},
						},
					),
				),
			},
			expectedResources: []client.Object{
				&rbacv1.Role{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Role",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace",
						Name:      "csv-service-account-one",
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{""},
							Resources: []string{"namespaces"},
							Verbs:     []string{"get", "list", "watch"},
						},
					},
				},
				&rbacv1.RoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "RoleBinding",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace",
						Name:      "csv-service-account-one",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							APIGroup:  "",
							Name:      "service-account-one",
							Namespace: "install-namespace",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     "csv-service-account-one",
					},
				},
				&rbacv1.Role{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Role",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace",
						Name:      "csv-service-account-two",
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{"appsv1"},
							Resources: []string{"deployments"},
							Verbs:     []string{"create"},
						},
					},
				},
				&rbacv1.RoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "RoleBinding",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace",
						Name:      "csv-service-account-two",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							APIGroup:  "",
							Name:      "service-account-two",
							Namespace: "install-namespace",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     "csv-service-account-two",
					},
				},
			},
		},
		{
			name: "treats empty service account as 'default' service account",
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{"watch-namespace"},
				UniqueNameGenerator: fakeUniqueNameGenerator,
			},
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithName("csv"),
					WithPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								},
							},
						},
					),
				),
			},
			expectedResources: []client.Object{
				&rbacv1.Role{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Role",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace",
						Name:      "csv-default",
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{""},
							Resources: []string{"namespaces"},
							Verbs:     []string{"get", "list", "watch"},
						},
					},
				},
				&rbacv1.RoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "RoleBinding",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "watch-namespace",
						Name:      "csv-default",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							APIGroup:  "",
							Name:      "default",
							Namespace: "install-namespace",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     "csv-default",
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objs, err := render.BundlePermissionsGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			for i := range objs {
				require.Equal(t, tc.expectedResources[i], objs[i], "failed to find expected resource at index %d", i)
			}
			require.Equal(t, len(tc.expectedResources), len(objs))
		})
	}
}

func Test_BundleClusterPermissionsGenerator(t *testing.T) {
	fakeUniqueNameGenerator := func(base string, _ interface{}) (string, error) {
		return base, nil
	}

	for _, tc := range []struct {
		name              string
		opts              render.Options
		bundle            *convert.RegistryV1
		expectedResources []client.Object
	}{
		{
			name: "promotes permissions to clusters permissions and adds namespace policy rule when in AllNamespaces mode (target namespace is [''])",
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{""},
				UniqueNameGenerator: fakeUniqueNameGenerator,
			},
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithName("csv"),
					WithPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-one",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								},
							},
						},
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-two",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{"appsv1"},
									Resources: []string{"deployments"},
									Verbs:     []string{"create"},
								},
							},
						},
					),
				),
			},
			expectedResources: []client.Object{
				&rbacv1.ClusterRole{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterRole",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "csv-service-account-one",
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{""},
							Resources: []string{"namespaces"},
							Verbs:     []string{"get", "list", "watch"},
						}, {
							Verbs:     []string{"get", "list", "watch"},
							APIGroups: []string{corev1.GroupName},
							Resources: []string{"namespaces"},
						},
					},
				},
				&rbacv1.ClusterRoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterRoleBinding",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "csv-service-account-one",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							APIGroup:  "",
							Name:      "service-account-one",
							Namespace: "install-namespace",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "ClusterRole",
						Name:     "csv-service-account-one",
					},
				},
				&rbacv1.ClusterRole{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterRole",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "csv-service-account-two",
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{"appsv1"},
							Resources: []string{"deployments"},
							Verbs:     []string{"create"},
						}, {
							Verbs:     []string{"get", "list", "watch"},
							APIGroups: []string{corev1.GroupName},
							Resources: []string{"namespaces"},
						},
					},
				},
				&rbacv1.ClusterRoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterRoleBinding",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "csv-service-account-two",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							APIGroup:  "",
							Name:      "service-account-two",
							Namespace: "install-namespace",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "ClusterRole",
						Name:     "csv-service-account-two",
					},
				},
			},
		},
		{
			name: "generates clusterroles and clusterrolebindings for clusterpermissions",
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{"watch-namespace"},
				UniqueNameGenerator: fakeUniqueNameGenerator,
			},
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithName("csv"),
					WithClusterPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-one",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								},
							},
						},
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-two",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{"appsv1"},
									Resources: []string{"deployments"},
									Verbs:     []string{"create"},
								},
							},
						},
					),
				),
			},
			expectedResources: []client.Object{
				&rbacv1.ClusterRole{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterRole",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "csv-service-account-one",
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{""},
							Resources: []string{"namespaces"},
							Verbs:     []string{"get", "list", "watch"},
						},
					},
				},
				&rbacv1.ClusterRoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterRoleBinding",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "csv-service-account-one",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							APIGroup:  "",
							Name:      "service-account-one",
							Namespace: "install-namespace",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "ClusterRole",
						Name:     "csv-service-account-one",
					},
				},
				&rbacv1.ClusterRole{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterRole",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "csv-service-account-two",
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{"appsv1"},
							Resources: []string{"deployments"},
							Verbs:     []string{"create"},
						},
					},
				},
				&rbacv1.ClusterRoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterRoleBinding",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "csv-service-account-two",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							APIGroup:  "",
							Name:      "service-account-two",
							Namespace: "install-namespace",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "ClusterRole",
						Name:     "csv-service-account-two",
					},
				},
			},
		},
		{
			name: "treats empty service accounts as 'default' service account",
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{"watch-namespace"},
				UniqueNameGenerator: fakeUniqueNameGenerator,
			},
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithName("csv"),
					WithClusterPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								},
							},
						},
					),
				),
			},
			expectedResources: []client.Object{
				&rbacv1.ClusterRole{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterRole",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "csv-default",
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{""},
							Resources: []string{"namespaces"},
							Verbs:     []string{"get", "list", "watch"},
						},
					},
				},
				&rbacv1.ClusterRoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterRoleBinding",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "csv-default",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							APIGroup:  "",
							Name:      "default",
							Namespace: "install-namespace",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "ClusterRole",
						Name:     "csv-default",
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objs, err := render.BundleClusterPermissionsGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			for i := range objs {
				require.Equal(t, tc.expectedResources[i], objs[i], "failed to find expected resource at index %d", i)
			}
			require.Equal(t, len(tc.expectedResources), len(objs))
		})
	}
}

func Test_BundleServiceAccountGenerator(t *testing.T) {
	for _, tc := range []struct {
		name              string
		opts              render.Options
		bundle            *convert.RegistryV1
		expectedResources []client.Object
	}{
		{
			name: "generates unique set of clusterpermissions and permissions service accounts in the install namespace",
			opts: render.Options{
				InstallNamespace: "install-namespace",
			},
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithName("csv"),
					WithPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-1",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								},
							},
						},
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-2",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{"appsv1"},
									Resources: []string{"deployments"},
									Verbs:     []string{"create"},
								},
							},
						},
					),
					WithClusterPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-2",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								},
							},
						},
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "service-account-3",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{"appsv1"},
									Resources: []string{"deployments"},
									Verbs:     []string{"create"},
								},
							},
						},
					),
				),
			},
			expectedResources: []client.Object{
				&corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ServiceAccount",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-account-1",
						Namespace: "install-namespace",
					},
				},
				&corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ServiceAccount",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-account-2",
						Namespace: "install-namespace",
					},
				},
				&corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ServiceAccount",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-account-3",
						Namespace: "install-namespace",
					},
				},
			},
		},
		{
			name: "treats empty service accounts as default and doesn't generate them",
			opts: render.Options{
				InstallNamespace: "install-namespace",
			},
			bundle: &convert.RegistryV1{
				CSV: MakeCSV(
					WithName("csv"),
					WithPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								},
							},
						},
					),
					WithClusterPermissions(
						v1alpha1.StrategyDeploymentPermissions{
							ServiceAccountName: "",
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list", "watch"},
								},
							},
						},
					),
				),
			},
			expectedResources: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objs, err := render.BundleServiceAccountGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			slices.SortFunc(objs, func(a, b client.Object) int {
				return cmp.Compare(a.GetName(), b.GetName())
			})
			for i := range objs {
				require.Equal(t, tc.expectedResources[i], objs[i], "failed to find expected resource at index %d", i)
			}
			require.Equal(t, len(tc.expectedResources), len(objs))
		})
	}
}

func Test_BundleCRDGenerator_Succeeds(t *testing.T) {
	opts := render.Options{
		InstallNamespace: "install-namespace",
		TargetNamespaces: []string{""},
	}

	bundle := &convert.RegistryV1{
		CRDs: []apiextensionsv1.CustomResourceDefinition{
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-one"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-two"}},
		},
	}

	objs, err := render.BundleCRDGenerator(bundle, opts)
	require.NoError(t, err)
	require.Equal(t, []client.Object{
		&apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "crd-one"}},
		&apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "crd-two"}},
	}, objs)
}

func Test_BundleResourceGenerator_Succeeds(t *testing.T) {
	opts := render.Options{
		InstallNamespace: "install-namespace",
	}

	bundle := &convert.RegistryV1{
		Others: []unstructured.Unstructured{
			toUnstructured(
				&corev1.Service{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Service",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "bundled-service",
					},
				},
			),
			toUnstructured(
				&rbacv1.ClusterRole{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterRole",
						APIVersion: rbacv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "bundled-clusterrole",
					},
				},
			),
		},
	}

	objs, err := render.BundleResourceGenerator(bundle, opts)
	require.NoError(t, err)
	require.Len(t, objs, 2)
}

type CSVOption func(version *v1alpha1.ClusterServiceVersion)

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

func WithOwnedCRDs(crdDesc ...v1alpha1.CRDDescription) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.CustomResourceDefinitions.Owned = crdDesc
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

func toUnstructured(obj client.Object) unstructured.Unstructured {
	gvk := obj.GetObjectKind().GroupVersionKind()

	var u unstructured.Unstructured
	uObj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	unstructured.RemoveNestedField(uObj, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(uObj, "status")
	u.Object = uObj
	u.SetGroupVersionKind(gvk)
	return u
}

func MakeCSV(opts ...CSVOption) v1alpha1.ClusterServiceVersion {
	csv := v1alpha1.ClusterServiceVersion{}
	for _, opt := range opts {
		opt(&csv)
	}
	return csv
}
