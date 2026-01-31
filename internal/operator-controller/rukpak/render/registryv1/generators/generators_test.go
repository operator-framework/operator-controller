package generators_test

import (
	"cmp"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/config"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/generators"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

func Test_ResourceGenerators(t *testing.T) {
	g := render.ResourceGenerators{
		func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
			return []client.Object{&corev1.Service{}}, nil
		},
		func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
			return []client.Object{&corev1.ConfigMap{}}, nil
		},
	}

	objs, err := g.GenerateResources(&bundle.RegistryV1{}, render.Options{})
	require.NoError(t, err)
	require.Equal(t, []client.Object{&corev1.Service{}, &corev1.ConfigMap{}}, objs)
}

func Test_ResourceGenerators_Errors(t *testing.T) {
	g := render.ResourceGenerators{
		func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
			return []client.Object{&corev1.Service{}}, nil
		},
		func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
			return nil, fmt.Errorf("generator error")
		},
	}

	objs, err := g.GenerateResources(&bundle.RegistryV1{}, render.Options{})
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "generator error")
}

func Test_BundleCSVDeploymentGenerator_Succeeds(t *testing.T) {
	for _, tc := range []struct {
		name              string
		bundle            *bundle.RegistryV1
		opts              render.Options
		expectedResources []client.Object
	}{
		{
			name: "generates deployment resources",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithAnnotations(map[string]string{
						"csv": "annotation",
					}).
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
									Spec: corev1.PodSpec{
										ServiceAccountName: "some-service-account",
									},
								},
							},
						},
						v1alpha1.StrategyDeploymentSpec{
							Name: "deployment-two",
							Spec: appsv1.DeploymentSpec{},
						},
					).Build(),
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
							Spec: corev1.PodSpec{
								ServiceAccountName: "some-service-account",
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
			objs, err := generators.BundleCSVDeploymentGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResources, objs)
		})
	}
}

func Test_BundleCSVDeploymentGenerator_WithCertWithCertProvider_Succeeds(t *testing.T) {
	fakeProvider := FakeCertProvider{
		GetCertSecretInfoFn: func(cfg render.CertificateProvisionerConfig) render.CertSecretInfo {
			return render.CertSecretInfo{
				SecretName:     "some-secret",
				CertificateKey: "some-cert-key",
				PrivateKeyKey:  "some-private-key-key",
			}
		},
	}

	b := &bundle.RegistryV1{
		CSV: clusterserviceversion.Builder().
			WithWebhookDefinitions(
				v1alpha1.WebhookDescription{
					Type:           v1alpha1.ValidatingAdmissionWebhook,
					DeploymentName: "deployment-one",
				}).
			// deployment must have a referencing webhook (or owned apiservice) definition to trigger cert secret
			WithStrategyDeploymentSpecs(
				v1alpha1.StrategyDeploymentSpec{
					Name: "deployment-one",
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Volumes: []corev1.Volume{
									// volume that have neither protected names: webhook-cert and apiservice-cert,
									// or target protected certificate paths should remain untouched
									{
										Name: "some-other-mount",
										VolumeSource: corev1.VolumeSource{
											EmptyDir: &corev1.EmptyDirVolumeSource{},
										},
									},
									// volume mounts with protected names will be rewritten to ensure they point to
									// the right certificate path. If they do not exist, they will be created.
									{
										Name: "webhook-cert",
										VolumeSource: corev1.VolumeSource{
											EmptyDir: &corev1.EmptyDirVolumeSource{},
										},
									},
									// volumes that point to protected paths will be removed
									{
										Name: "some-mount",
										VolumeSource: corev1.VolumeSource{
											EmptyDir: &corev1.EmptyDirVolumeSource{},
										},
									},
									{
										Name: "some-webhook-cert-mount",
										VolumeSource: corev1.VolumeSource{
											EmptyDir: &corev1.EmptyDirVolumeSource{},
										},
									},
								},
								Containers: []corev1.Container{
									{
										Name: "container-1",
										VolumeMounts: []corev1.VolumeMount{
											// the mount path for the following volume will be replaced
											// since the volume name is protected
											{
												Name:      "webhook-cert",
												MountPath: "/webhook-cert-path",
											},
											// the following volume will be preserved
											{
												Name:      "some-other-mount",
												MountPath: "/some/other/mount/path",
											},
											// these volume mount will be removed for referencing protected cert paths
											{
												Name:      "some-webhook-cert-mount",
												MountPath: "/tmp/k8s-webhook-server/serving-certs",
											}, {
												Name:      "some-mount",
												MountPath: "/apiserver.local.config/certificates",
											},
										},
									},
									{
										Name: "container-2",
										// expect cert volumes to be injected
									},
								},
							},
						},
					},
				},
			).Build(),
	}

	objs, err := generators.BundleCSVDeploymentGenerator(b, render.Options{
		InstallNamespace:    "install-namespace",
		CertificateProvider: fakeProvider,
	})
	require.NoError(t, err)
	require.Len(t, objs, 1)

	deployment := objs[0].(*appsv1.Deployment)
	require.NotNil(t, deployment)

	require.Equal(t, []corev1.Volume{
		{
			Name: "some-other-mount",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "webhook-cert",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "some-secret",
					Items: []corev1.KeyToPath{
						{
							Key:  "some-cert-key",
							Path: "tls.crt",
						},
						{
							Key:  "some-private-key-key",
							Path: "tls.key",
						},
					},
				},
			},
		},
		{
			Name: "apiservice-cert",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "some-secret",
					Items: []corev1.KeyToPath{
						{
							Key:  "some-cert-key",
							Path: "apiserver.crt",
						},
						{
							Key:  "some-private-key-key",
							Path: "apiserver.key",
						},
					},
				},
			},
		},
	}, deployment.Spec.Template.Spec.Volumes)
	require.Equal(t, []corev1.Container{
		{
			Name: "container-1",
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "some-other-mount",
					MountPath: "/some/other/mount/path",
				},
				{
					Name:      "webhook-cert",
					MountPath: "/tmp/k8s-webhook-server/serving-certs",
				},
				{
					Name:      "apiservice-cert",
					MountPath: "/apiserver.local.config/certificates",
				},
			},
		},
		{
			Name: "container-2",
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "webhook-cert",
					MountPath: "/tmp/k8s-webhook-server/serving-certs",
				},
				{
					Name:      "apiservice-cert",
					MountPath: "/apiserver.local.config/certificates",
				},
			},
		},
	}, deployment.Spec.Template.Spec.Containers)
}

func Test_BundleCSVDeploymentGenerator_FailsOnNil(t *testing.T) {
	objs, err := generators.BundleCSVDeploymentGenerator(nil, render.Options{})
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle cannot be nil")
}

func Test_BundleCSVPermissionsGenerator_Succeeds(t *testing.T) {
	fakeUniqueNameGenerator := func(base string, _ interface{}) string {
		return base
	}

	for _, tc := range []struct {
		name              string
		opts              render.Options
		bundle            *bundle.RegistryV1
		expectedResources []client.Object
	}{
		{
			name: "does not generate any resources when in AllNamespaces mode (target namespace is [''])",
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{""},
				UniqueNameGenerator: fakeUniqueNameGenerator,
			},
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("csv").
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
					).Build(),
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
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("csv").
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
					).Build(),
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
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("csv").
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
					).Build(),
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
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("csv").
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
					).Build(),
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
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("csv").
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
					).Build(),
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
			objs, err := generators.BundleCSVPermissionsGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			for i := range objs {
				require.Equal(t, tc.expectedResources[i], objs[i], "failed to find expected resource at index %d", i)
			}
			require.Len(t, objs, len(tc.expectedResources))
		})
	}
}

func Test_BundleCSVPermissionGenerator_FailsOnNil(t *testing.T) {
	objs, err := generators.BundleCSVPermissionsGenerator(nil, render.Options{})
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle cannot be nil")
}

func Test_BundleCSVClusterPermissionsGenerator_Succeeds(t *testing.T) {
	fakeUniqueNameGenerator := func(base string, _ interface{}) string {
		return base
	}

	for _, tc := range []struct {
		name              string
		opts              render.Options
		bundle            *bundle.RegistryV1
		expectedResources []client.Object
	}{
		{
			name: "promotes permissions to clusters permissions and adds namespace policy rule when in AllNamespaces mode (target namespace is [''])",
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{""},
				UniqueNameGenerator: fakeUniqueNameGenerator,
			},
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("csv").
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
					).Build(),
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
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("csv").
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
					).Build(),
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
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("csv").
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
					).Build(),
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
			objs, err := generators.BundleCSVClusterPermissionsGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			for i := range objs {
				require.Equal(t, tc.expectedResources[i], objs[i], "failed to find expected resource at index %d", i)
			}
			require.Len(t, objs, len(tc.expectedResources))
		})
	}
}

func Test_BundleCSVClusterPermissionGenerator_FailsOnNil(t *testing.T) {
	objs, err := generators.BundleCSVClusterPermissionsGenerator(nil, render.Options{})
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle cannot be nil")
}

func Test_BundleCSVServiceAccountGenerator_Succeeds(t *testing.T) {
	for _, tc := range []struct {
		name              string
		opts              render.Options
		bundle            *bundle.RegistryV1
		expectedResources []client.Object
	}{
		{
			name: "generates unique set of clusterpermissions and permissions service accounts in the install namespace",
			opts: render.Options{
				InstallNamespace: "install-namespace",
			},
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("csv").
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
					).
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
					).Build(),
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
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithName("csv").
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
					).
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
					).Build(),
			},
			expectedResources: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objs, err := generators.BundleCSVServiceAccountGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			slices.SortFunc(objs, func(a, b client.Object) int {
				return cmp.Compare(a.GetName(), b.GetName())
			})
			for i := range objs {
				require.Equal(t, tc.expectedResources[i], objs[i], "failed to find expected resource at index %d", i)
			}
			require.Len(t, objs, len(tc.expectedResources))
		})
	}
}

func Test_BundleCSVServiceAccountGenerator_FailsOnNil(t *testing.T) {
	objs, err := generators.BundleCSVServiceAccountGenerator(nil, render.Options{})
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle cannot be nil")
}

func Test_BundleCRDGenerator_Succeeds(t *testing.T) {
	opts := render.Options{
		InstallNamespace: "install-namespace",
		TargetNamespaces: []string{""},
	}

	bundle := &bundle.RegistryV1{
		CRDs: []apiextensionsv1.CustomResourceDefinition{
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-one"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-two"}},
		},
	}

	objs, err := generators.BundleCRDGenerator(bundle, opts)
	require.NoError(t, err)
	require.Equal(t, []client.Object{
		&apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "crd-one"}},
		&apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "crd-two"}},
	}, objs)
}

func Test_BundleCRDGenerator_WithConversionWebhook_Succeeds(t *testing.T) {
	opts := render.Options{
		InstallNamespace: "install-namespace",
		TargetNamespaces: []string{""},
	}

	bundle := &bundle.RegistryV1{
		CRDs: []apiextensionsv1.CustomResourceDefinition{
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-one"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-two"}},
		},
		CSV: clusterserviceversion.Builder().
			WithWebhookDefinitions(
				v1alpha1.WebhookDescription{
					Type:                    v1alpha1.ConversionWebhook,
					WebhookPath:             ptr.To("/some/path"),
					ContainerPort:           8443,
					AdmissionReviewVersions: []string{"v1", "v1beta1"},
					ConversionCRDs:          []string{"crd-one"},
					DeploymentName:          "some-deployment",
				},
				v1alpha1.WebhookDescription{
					// should use / as WebhookPath by default
					Type:                    v1alpha1.ConversionWebhook,
					ContainerPort:           8443,
					AdmissionReviewVersions: []string{"v1", "v1beta1"},
					ConversionCRDs:          []string{"crd-two"},
					DeploymentName:          "some-deployment",
				},
			).Build(),
	}

	objs, err := generators.BundleCRDGenerator(bundle, opts)
	require.NoError(t, err)
	require.Equal(t, []client.Object{
		&apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: "crd-one",
			},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Conversion: &apiextensionsv1.CustomResourceConversion{
					Strategy: apiextensionsv1.WebhookConverter,
					Webhook: &apiextensionsv1.WebhookConversion{
						ClientConfig: &apiextensionsv1.WebhookClientConfig{
							Service: &apiextensionsv1.ServiceReference{
								Namespace: "install-namespace",
								Name:      "some-deployment-service",
								Path:      ptr.To("/some/path"),
								Port:      ptr.To(int32(8443)),
							},
						},
						ConversionReviewVersions: []string{"v1", "v1beta1"},
					},
				},
			},
		},
		&apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: "crd-two",
			},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Conversion: &apiextensionsv1.CustomResourceConversion{
					Strategy: apiextensionsv1.WebhookConverter,
					Webhook: &apiextensionsv1.WebhookConversion{
						ClientConfig: &apiextensionsv1.WebhookClientConfig{
							Service: &apiextensionsv1.ServiceReference{
								Namespace: "install-namespace",
								Name:      "some-deployment-service",
								Path:      ptr.To("/"),
								Port:      ptr.To(int32(8443)),
							},
						},
						ConversionReviewVersions: []string{"v1", "v1beta1"},
					},
				},
			},
		},
	}, objs)
}

func Test_BundleCRDGenerator_WithConversionWebhook_Fails(t *testing.T) {
	opts := render.Options{
		InstallNamespace: "install-namespace",
		TargetNamespaces: []string{""},
	}

	bundle := &bundle.RegistryV1{
		CRDs: []apiextensionsv1.CustomResourceDefinition{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "crd-one"},
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					PreserveUnknownFields: true,
				},
			},
		},
		CSV: clusterserviceversion.Builder().
			WithWebhookDefinitions(
				v1alpha1.WebhookDescription{
					Type:                    v1alpha1.ConversionWebhook,
					WebhookPath:             ptr.To("/some/path"),
					ContainerPort:           8443,
					AdmissionReviewVersions: []string{"v1", "v1beta1"},
					ConversionCRDs:          []string{"crd-one"},
					DeploymentName:          "some-deployment",
				},
			).Build(),
	}

	objs, err := generators.BundleCRDGenerator(bundle, opts)
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must have .spec.preserveUnknownFields set to false to let API Server call webhook to do the conversion")
}

func Test_BundleCRDGenerator_WithCertProvider_Succeeds(t *testing.T) {
	fakeProvider := FakeCertProvider{
		InjectCABundleFn: func(obj client.Object, cfg render.CertificateProvisionerConfig) error {
			obj.SetAnnotations(map[string]string{
				"cert-provider": "annotation",
			})
			return nil
		},
	}

	opts := render.Options{
		InstallNamespace:    "install-namespace",
		TargetNamespaces:    []string{""},
		CertificateProvider: fakeProvider,
	}

	bundle := &bundle.RegistryV1{
		CRDs: []apiextensionsv1.CustomResourceDefinition{
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-one"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-two"}},
		},
		CSV: clusterserviceversion.Builder().
			WithWebhookDefinitions(
				v1alpha1.WebhookDescription{
					Type:           v1alpha1.ConversionWebhook,
					DeploymentName: "my-deployment",
					ConversionCRDs: []string{
						"crd-one",
					},
				},
			).Build(),
	}

	objs, err := generators.BundleCRDGenerator(bundle, opts)
	require.NoError(t, err)
	require.Len(t, objs, 2)
	require.Equal(t, map[string]string{
		"cert-provider": "annotation",
	}, objs[0].GetAnnotations())
}

func Test_BundleCRDGenerator_FailsOnNil(t *testing.T) {
	objs, err := generators.BundleCRDGenerator(nil, render.Options{})
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle cannot be nil")
}

func Test_BundleAdditionalResourcesGenerator_Succeeds(t *testing.T) {
	opts := render.Options{
		InstallNamespace: "install-namespace",
	}

	bundle := &bundle.RegistryV1{
		Others: []unstructured.Unstructured{
			*ToUnstructuredT(t,
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
			*ToUnstructuredT(t,
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

	objs, err := generators.BundleAdditionalResourcesGenerator(bundle, opts)
	require.NoError(t, err)
	require.Len(t, objs, 2)
}

func Test_BundleAdditionalResourcesGenerator_FailsOnNil(t *testing.T) {
	objs, err := generators.BundleAdditionalResourcesGenerator(nil, render.Options{})
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle cannot be nil")
}

func Test_BundleValidatingWebhookResourceGenerator_Succeeds(t *testing.T) {
	fakeProvider := FakeCertProvider{
		InjectCABundleFn: func(obj client.Object, cfg render.CertificateProvisionerConfig) error {
			obj.SetAnnotations(map[string]string{
				"cert-provider": "annotation",
			})
			return nil
		},
	}
	for _, tc := range []struct {
		name              string
		bundle            *bundle.RegistryV1
		opts              render.Options
		expectedResources []client.Object
	}{
		{
			name: "generates validating webhook configuration resources described in the bundle's cluster service version",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ValidatingAdmissionWebhook,
							GenerateName:   "my-webhook",
							DeploymentName: "my-deployment",
							Rules: []admissionregistrationv1.RuleWithOperations{
								{
									Operations: []admissionregistrationv1.OperationType{
										admissionregistrationv1.OperationAll,
									},
									Rule: admissionregistrationv1.Rule{
										APIGroups:   []string{""},
										APIVersions: []string{""},
										Resources:   []string{"namespaces"},
									},
								},
							},
							FailurePolicy: ptr.To(admissionregistrationv1.Fail),
							ObjectSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							SideEffects:    ptr.To(admissionregistrationv1.SideEffectClassNone),
							TimeoutSeconds: ptr.To(int32(1)),
							AdmissionReviewVersions: []string{
								"v1beta1",
								"v1beta2",
							},
							WebhookPath:   ptr.To("/webhook-path"),
							ContainerPort: 443,
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "install-namespace",
				TargetNamespaces: []string{""},
			},
			expectedResources: []client.Object{
				&admissionregistrationv1.ValidatingWebhookConfiguration{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ValidatingWebhookConfiguration",
						APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-webhook",
						Namespace: "install-namespace",
					},
					Webhooks: []admissionregistrationv1.ValidatingWebhook{
						{
							Name: "my-webhook",
							Rules: []admissionregistrationv1.RuleWithOperations{
								{
									Operations: []admissionregistrationv1.OperationType{
										admissionregistrationv1.OperationAll,
									},
									Rule: admissionregistrationv1.Rule{
										APIGroups:   []string{""},
										APIVersions: []string{""},
										Resources:   []string{"namespaces"},
									},
								},
							},
							FailurePolicy: ptr.To(admissionregistrationv1.Fail),
							ObjectSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							SideEffects:    ptr.To(admissionregistrationv1.SideEffectClassNone),
							TimeoutSeconds: ptr.To(int32(1)),
							AdmissionReviewVersions: []string{
								"v1beta1",
								"v1beta2",
							},
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Namespace: "install-namespace",
									Name:      "my-deployment-service",
									Path:      ptr.To("/webhook-path"),
									Port:      ptr.To(int32(443)),
								},
							},
							// No NamespaceSelector is set targetNamespaces = []string{""} (AllNamespaces install mode)
						},
					},
				},
			},
		},
		{
			name: "removes any - suffixes from the webhook name (v0 used GenerateName to allow multiple operator installations - we don't want that in v1)",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ValidatingAdmissionWebhook,
							GenerateName:   "my-webhook-",
							DeploymentName: "my-deployment",
							Rules: []admissionregistrationv1.RuleWithOperations{
								{
									Operations: []admissionregistrationv1.OperationType{
										admissionregistrationv1.OperationAll,
									},
									Rule: admissionregistrationv1.Rule{
										APIGroups:   []string{""},
										APIVersions: []string{""},
										Resources:   []string{"namespaces"},
									},
								},
							},
							FailurePolicy: ptr.To(admissionregistrationv1.Fail),
							ObjectSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							SideEffects:    ptr.To(admissionregistrationv1.SideEffectClassNone),
							TimeoutSeconds: ptr.To(int32(1)),
							AdmissionReviewVersions: []string{
								"v1beta1",
								"v1beta2",
							},
							WebhookPath:   ptr.To("/webhook-path"),
							ContainerPort: 443,
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "install-namespace",
				TargetNamespaces: []string{"watch-namespace-one", "watch-namespace-two"},
			},
			expectedResources: []client.Object{
				&admissionregistrationv1.ValidatingWebhookConfiguration{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ValidatingWebhookConfiguration",
						APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-webhook",
						Namespace: "install-namespace",
					},
					Webhooks: []admissionregistrationv1.ValidatingWebhook{
						{
							Name: "my-webhook",
							Rules: []admissionregistrationv1.RuleWithOperations{
								{
									Operations: []admissionregistrationv1.OperationType{
										admissionregistrationv1.OperationAll,
									},
									Rule: admissionregistrationv1.Rule{
										APIGroups:   []string{""},
										APIVersions: []string{""},
										Resources:   []string{"namespaces"},
									},
								},
							},
							FailurePolicy: ptr.To(admissionregistrationv1.Fail),
							ObjectSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							SideEffects:    ptr.To(admissionregistrationv1.SideEffectClassNone),
							TimeoutSeconds: ptr.To(int32(1)),
							AdmissionReviewVersions: []string{
								"v1beta1",
								"v1beta2",
							},
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Namespace: "install-namespace",
									Name:      "my-deployment-service",
									Path:      ptr.To("/webhook-path"),
									Port:      ptr.To(int32(443)),
								},
							},
							NamespaceSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "kubernetes.io/metadata.name",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"watch-namespace-one", "watch-namespace-two"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "generates validating webhook configuration resources with certificate provider modifications",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ValidatingAdmissionWebhook,
							GenerateName:   "my-webhook",
							DeploymentName: "my-deployment",
							ContainerPort:  443,
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{"watch-namespace-one", "watch-namespace-two"},
				CertificateProvider: fakeProvider,
			},
			expectedResources: []client.Object{
				&admissionregistrationv1.ValidatingWebhookConfiguration{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ValidatingWebhookConfiguration",
						APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-webhook",
						Namespace: "install-namespace",
						Annotations: map[string]string{
							"cert-provider": "annotation",
						},
					},
					Webhooks: []admissionregistrationv1.ValidatingWebhook{
						{
							Name: "my-webhook",
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Namespace: "install-namespace",
									Name:      "my-deployment-service",
									Port:      ptr.To(int32(443)),
								},
							},
							NamespaceSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "kubernetes.io/metadata.name",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"watch-namespace-one", "watch-namespace-two"},
									},
								},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objs, err := generators.BundleValidatingWebhookResourceGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResources, objs)
		})
	}
}

func Test_BundleValidatingWebhookResourceGenerator_FailsOnNil(t *testing.T) {
	objs, err := generators.BundleValidatingWebhookResourceGenerator(nil, render.Options{})
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle cannot be nil")
}

func Test_BundleMutatingWebhookResourceGenerator_Succeeds(t *testing.T) {
	fakeProvider := FakeCertProvider{
		InjectCABundleFn: func(obj client.Object, cfg render.CertificateProvisionerConfig) error {
			obj.SetAnnotations(map[string]string{
				"cert-provider": "annotation",
			})
			return nil
		},
	}
	for _, tc := range []struct {
		name              string
		bundle            *bundle.RegistryV1
		opts              render.Options
		expectedResources []client.Object
	}{
		{
			name: "generates validating webhook configuration resources described in the bundle's cluster service version",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.MutatingAdmissionWebhook,
							GenerateName:   "my-webhook",
							DeploymentName: "my-deployment",
							Rules: []admissionregistrationv1.RuleWithOperations{
								{
									Operations: []admissionregistrationv1.OperationType{
										admissionregistrationv1.OperationAll,
									},
									Rule: admissionregistrationv1.Rule{
										APIGroups:   []string{""},
										APIVersions: []string{""},
										Resources:   []string{"namespaces"},
									},
								},
							},
							FailurePolicy: ptr.To(admissionregistrationv1.Fail),
							ObjectSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							SideEffects:    ptr.To(admissionregistrationv1.SideEffectClassNone),
							TimeoutSeconds: ptr.To(int32(1)),
							AdmissionReviewVersions: []string{
								"v1beta1",
								"v1beta2",
							},
							WebhookPath:        ptr.To("/webhook-path"),
							ContainerPort:      443,
							ReinvocationPolicy: ptr.To(admissionregistrationv1.IfNeededReinvocationPolicy),
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "install-namespace",
				TargetNamespaces: []string{""},
			},
			expectedResources: []client.Object{
				&admissionregistrationv1.MutatingWebhookConfiguration{
					TypeMeta: metav1.TypeMeta{
						Kind:       "MutatingWebhookConfiguration",
						APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-webhook",
						Namespace: "install-namespace",
					},
					Webhooks: []admissionregistrationv1.MutatingWebhook{
						{
							Name: "my-webhook",
							Rules: []admissionregistrationv1.RuleWithOperations{
								{
									Operations: []admissionregistrationv1.OperationType{
										admissionregistrationv1.OperationAll,
									},
									Rule: admissionregistrationv1.Rule{
										APIGroups:   []string{""},
										APIVersions: []string{""},
										Resources:   []string{"namespaces"},
									},
								},
							},
							FailurePolicy: ptr.To(admissionregistrationv1.Fail),
							ObjectSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							SideEffects:    ptr.To(admissionregistrationv1.SideEffectClassNone),
							TimeoutSeconds: ptr.To(int32(1)),
							AdmissionReviewVersions: []string{
								"v1beta1",
								"v1beta2",
							},
							ReinvocationPolicy: ptr.To(admissionregistrationv1.IfNeededReinvocationPolicy),
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Namespace: "install-namespace",
									Name:      "my-deployment-service",
									Path:      ptr.To("/webhook-path"),
									Port:      ptr.To(int32(443)),
								},
							},
							// No NamespaceSelector is set targetNamespaces = []string{""} (AllNamespaces install mode)
						},
					},
				},
			},
		},
		{
			name: "removes any - suffixes from the webhook name (v0 used GenerateName to allow multiple operator installations - we don't want that in v1)",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.MutatingAdmissionWebhook,
							GenerateName:   "my-webhook-",
							DeploymentName: "my-deployment",
							Rules: []admissionregistrationv1.RuleWithOperations{
								{
									Operations: []admissionregistrationv1.OperationType{
										admissionregistrationv1.OperationAll,
									},
									Rule: admissionregistrationv1.Rule{
										APIGroups:   []string{""},
										APIVersions: []string{""},
										Resources:   []string{"namespaces"},
									},
								},
							},
							FailurePolicy: ptr.To(admissionregistrationv1.Fail),
							ObjectSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							SideEffects:    ptr.To(admissionregistrationv1.SideEffectClassNone),
							TimeoutSeconds: ptr.To(int32(1)),
							AdmissionReviewVersions: []string{
								"v1beta1",
								"v1beta2",
							},
							WebhookPath:        ptr.To("/webhook-path"),
							ContainerPort:      443,
							ReinvocationPolicy: ptr.To(admissionregistrationv1.IfNeededReinvocationPolicy),
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "install-namespace",
				TargetNamespaces: []string{"watch-namespace-one", "watch-namespace-two"},
			},
			expectedResources: []client.Object{
				&admissionregistrationv1.MutatingWebhookConfiguration{
					TypeMeta: metav1.TypeMeta{
						Kind:       "MutatingWebhookConfiguration",
						APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-webhook",
						Namespace: "install-namespace",
					},
					Webhooks: []admissionregistrationv1.MutatingWebhook{
						{
							Name: "my-webhook",
							Rules: []admissionregistrationv1.RuleWithOperations{
								{
									Operations: []admissionregistrationv1.OperationType{
										admissionregistrationv1.OperationAll,
									},
									Rule: admissionregistrationv1.Rule{
										APIGroups:   []string{""},
										APIVersions: []string{""},
										Resources:   []string{"namespaces"},
									},
								},
							},
							FailurePolicy: ptr.To(admissionregistrationv1.Fail),
							ObjectSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							SideEffects:    ptr.To(admissionregistrationv1.SideEffectClassNone),
							TimeoutSeconds: ptr.To(int32(1)),
							AdmissionReviewVersions: []string{
								"v1beta1",
								"v1beta2",
							},
							ReinvocationPolicy: ptr.To(admissionregistrationv1.IfNeededReinvocationPolicy),
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Namespace: "install-namespace",
									Name:      "my-deployment-service",
									Path:      ptr.To("/webhook-path"),
									Port:      ptr.To(int32(443)),
								},
							},
							NamespaceSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "kubernetes.io/metadata.name",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"watch-namespace-one", "watch-namespace-two"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "generates validating webhook configuration resources with certificate provider modifications",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.MutatingAdmissionWebhook,
							GenerateName:   "my-webhook",
							DeploymentName: "my-deployment",
							ContainerPort:  443,
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{"watch-namespace-one", "watch-namespace-two"},
				CertificateProvider: fakeProvider,
			},
			expectedResources: []client.Object{
				&admissionregistrationv1.MutatingWebhookConfiguration{
					TypeMeta: metav1.TypeMeta{
						Kind:       "MutatingWebhookConfiguration",
						APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-webhook",
						Namespace: "install-namespace",
						Annotations: map[string]string{
							"cert-provider": "annotation",
						},
					},
					Webhooks: []admissionregistrationv1.MutatingWebhook{
						{
							Name: "my-webhook",
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Namespace: "install-namespace",
									Name:      "my-deployment-service",
									Port:      ptr.To(int32(443)),
								},
							},
							NamespaceSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "kubernetes.io/metadata.name",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"watch-namespace-one", "watch-namespace-two"},
									},
								},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objs, err := generators.BundleMutatingWebhookResourceGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResources, objs)
		})
	}
}

func Test_BundleMutatingWebhookResourceGenerator_FailsOnNil(t *testing.T) {
	objs, err := generators.BundleMutatingWebhookResourceGenerator(nil, render.Options{})
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle cannot be nil")
}

func Test_BundleDeploymentServiceResourceGenerator_Succeeds(t *testing.T) {
	fakeProvider := FakeCertProvider{
		InjectCABundleFn: func(obj client.Object, cfg render.CertificateProvisionerConfig) error {
			obj.SetAnnotations(map[string]string{
				"cert-provider": "annotation",
			})
			return nil
		},
	}
	for _, tc := range []struct {
		name              string
		bundle            *bundle.RegistryV1
		opts              render.Options
		expectedResources []client.Object
	}{
		{
			name: "generates webhook services using container port 443 and target port 443 by default",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "my-deployment",
						}).
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.MutatingAdmissionWebhook,
							DeploymentName: "my-deployment",
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "install-namespace",
				TargetNamespaces: []string{"watch-namespace-one", "watch-namespace-two"},
			},
			expectedResources: []client.Object{
				&corev1.Service{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Service",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-deployment-service",
						Namespace: "install-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name: "443",
								Port: int32(443),
								TargetPort: intstr.IntOrString{
									Type:   intstr.Int,
									IntVal: 443,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "generates webhook services using the given container port and setting target port the same as the container port if not given",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "my-deployment",
						}).
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ValidatingAdmissionWebhook,
							DeploymentName: "my-deployment",
							ContainerPort:  int32(8443),
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "install-namespace",
				TargetNamespaces: []string{"watch-namespace-one", "watch-namespace-two"},
			},
			expectedResources: []client.Object{
				&corev1.Service{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Service",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-deployment-service",
						Namespace: "install-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name: "8443",
								Port: int32(8443),
								TargetPort: intstr.IntOrString{
									Type:   intstr.Int,
									IntVal: 8443,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "generates webhook services using given container port of 443 and given target port",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "my-deployment",
						}).
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ConversionWebhook,
							DeploymentName: "my-deployment",
							TargetPort: &intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 8080,
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "install-namespace",
				TargetNamespaces: []string{"watch-namespace-one", "watch-namespace-two"},
			},
			expectedResources: []client.Object{
				&corev1.Service{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Service",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-deployment-service",
						Namespace: "install-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name: "443",
								Port: int32(443),
								TargetPort: intstr.IntOrString{
									Type:   intstr.Int,
									IntVal: 8080,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "generates webhook services using given container port and target port",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "my-deployment",
						}).
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ConversionWebhook,
							DeploymentName: "my-deployment",
							ContainerPort:  int32(9090),
							TargetPort: &intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 9099,
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "install-namespace",
				TargetNamespaces: []string{"watch-namespace-one", "watch-namespace-two"},
			},
			expectedResources: []client.Object{
				&corev1.Service{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Service",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-deployment-service",
						Namespace: "install-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name: "9090",
								Port: int32(9090),
								TargetPort: intstr.IntOrString{
									Type:   intstr.Int,
									IntVal: 9099,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "generates webhook services using referenced deployment defined label selector",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "my-deployment",
							Spec: appsv1.DeploymentSpec{
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"foo": "bar",
									},
								},
							},
						}).
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ConversionWebhook,
							DeploymentName: "my-deployment",
							ContainerPort:  int32(9090),
							TargetPort: &intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 9099,
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "install-namespace",
				TargetNamespaces: []string{"watch-namespace-one", "watch-namespace-two"},
			},
			expectedResources: []client.Object{
				&corev1.Service{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Service",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-deployment-service",
						Namespace: "install-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name: "9090",
								Port: int32(9090),
								TargetPort: intstr.IntOrString{
									Type:   intstr.Int,
									IntVal: 9099,
								},
							},
						},
						Selector: map[string]string{
							"foo": "bar",
						},
					},
				},
			},
		},
		{
			name: "aggregates all webhook definitions referencing the same deployment into a single service",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "my-deployment",
							Spec: appsv1.DeploymentSpec{
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"foo": "bar",
									},
								},
							},
						}).
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.MutatingAdmissionWebhook,
							DeploymentName: "my-deployment",
						},
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ValidatingAdmissionWebhook,
							DeploymentName: "my-deployment",
							ContainerPort:  int32(8443),
						},
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ConversionWebhook,
							DeploymentName: "my-deployment",
							TargetPort: &intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 8080,
							},
						},
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.ConversionWebhook,
							DeploymentName: "my-deployment",
							ContainerPort:  int32(9090),
							TargetPort: &intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 9099,
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "install-namespace",
				TargetNamespaces: []string{"watch-namespace-one", "watch-namespace-two"},
			},
			expectedResources: []client.Object{
				&corev1.Service{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Service",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-deployment-service",
						Namespace: "install-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name: "443",
								Port: int32(443),
								TargetPort: intstr.IntOrString{
									Type:   intstr.Int,
									IntVal: 443,
								},
							}, {
								Name: "443",
								Port: int32(443),
								TargetPort: intstr.IntOrString{
									Type:   intstr.Int,
									IntVal: 8080,
								},
							}, {
								Name: "8443",
								Port: int32(8443),
								TargetPort: intstr.IntOrString{
									Type:   intstr.Int,
									IntVal: 8443,
								},
							}, {
								Name: "9090",
								Port: int32(9090),
								TargetPort: intstr.IntOrString{
									Type:   intstr.Int,
									IntVal: 9099,
								},
							},
						},
						Selector: map[string]string{
							"foo": "bar",
						},
					},
				},
			},
		},
		{
			name: "applies cert provider modifiers to webhook service",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "my-deployment",
						}).
					WithWebhookDefinitions(
						v1alpha1.WebhookDescription{
							Type:           v1alpha1.MutatingAdmissionWebhook,
							DeploymentName: "my-deployment",
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace:    "install-namespace",
				TargetNamespaces:    []string{"watch-namespace-one", "watch-namespace-two"},
				CertificateProvider: fakeProvider,
			},
			expectedResources: []client.Object{
				&corev1.Service{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Service",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-deployment-service",
						Namespace: "install-namespace",
						Annotations: map[string]string{
							"cert-provider": "annotation",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name: "443",
								Port: int32(443),
								TargetPort: intstr.IntOrString{
									Type:   intstr.Int,
									IntVal: 443,
								},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objs, err := generators.BundleDeploymentServiceResourceGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResources, objs)
		})
	}
}

func Test_BundleDeploymentServiceResourceGenerator_FailsOnNil(t *testing.T) {
	objs, err := generators.BundleMutatingWebhookResourceGenerator(nil, render.Options{})
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle cannot be nil")
}

func Test_CertProviderResourceGenerator_Succeeds(t *testing.T) {
	fakeProvider := FakeCertProvider{
		AdditionalObjectsFn: func(cfg render.CertificateProvisionerConfig) ([]unstructured.Unstructured, error) {
			return []unstructured.Unstructured{*ToUnstructuredT(t, &corev1.Secret{
				TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: corev1.SchemeGroupVersion.String()},
				ObjectMeta: metav1.ObjectMeta{
					Name: cfg.CertName,
				},
			})}, nil
		},
	}

	objs, err := generators.CertProviderResourceGenerator(&bundle.RegistryV1{
		CSV: clusterserviceversion.Builder().
			WithWebhookDefinitions(
				// only generate resources for deployments referenced by webhook definitions
				v1alpha1.WebhookDescription{
					Type:           v1alpha1.MutatingAdmissionWebhook,
					DeploymentName: "my-deployment",
				},
			).
			WithStrategyDeploymentSpecs(
				v1alpha1.StrategyDeploymentSpec{
					Name: "my-deployment",
				},
				v1alpha1.StrategyDeploymentSpec{
					Name: "my-other-deployment",
				},
			).Build(),
	}, render.Options{
		InstallNamespace:    "install-namespace",
		CertificateProvider: fakeProvider,
	})
	require.NoError(t, err)
	require.Equal(t, []client.Object{
		ToUnstructuredT(t, &corev1.Secret{
			TypeMeta:   metav1.TypeMeta{Kind: "Secret", APIVersion: corev1.SchemeGroupVersion.String()},
			ObjectMeta: metav1.ObjectMeta{Name: "my-deployment-service-cert"},
		}),
	}, objs)
}

func Test_BundleCSVDeploymentGenerator_WithDeploymentConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bundle *bundle.RegistryV1
		opts   render.Options
		verify func(*testing.T, []client.Object)
	}{
		{
			name: "applies env vars from deployment config",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "test-deployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name: "manager",
												Env: []corev1.EnvVar{
													{Name: "EXISTING_VAR", Value: "existing_value"},
												},
											},
										},
									},
								},
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "test-ns",
				TargetNamespaces: []string{"test-ns"},
				DeploymentConfig: &config.DeploymentConfig{
					Env: []corev1.EnvVar{
						{Name: "NEW_VAR", Value: "new_value"},
						{Name: "EXISTING_VAR", Value: "overridden_value"},
					},
				},
			},
			verify: func(t *testing.T, objs []client.Object) {
				require.Len(t, objs, 1)
				dep := objs[0].(*appsv1.Deployment)
				require.Len(t, dep.Spec.Template.Spec.Containers, 1)
				envVars := dep.Spec.Template.Spec.Containers[0].Env

				// Should have both vars
				require.Len(t, envVars, 2)

				// Existing var should be overridden
				var existingVar *corev1.EnvVar
				for i := range envVars {
					if envVars[i].Name == "EXISTING_VAR" {
						existingVar = &envVars[i]
						break
					}
				}
				require.NotNil(t, existingVar)
				require.Equal(t, "overridden_value", existingVar.Value)

				// New var should be added
				var newVar *corev1.EnvVar
				for i := range envVars {
					if envVars[i].Name == "NEW_VAR" {
						newVar = &envVars[i]
						break
					}
				}
				require.NotNil(t, newVar)
				require.Equal(t, "new_value", newVar.Value)
			},
		},
		{
			name: "applies resources from deployment config",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "test-deployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{Name: "manager"},
										},
									},
								},
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "test-ns",
				TargetNamespaces: []string{"test-ns"},
				DeploymentConfig: &config.DeploymentConfig{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			},
			verify: func(t *testing.T, objs []client.Object) {
				require.Len(t, objs, 1)
				dep := objs[0].(*appsv1.Deployment)
				resources := dep.Spec.Template.Spec.Containers[0].Resources

				require.Equal(t, resource.MustParse("100m"), *resources.Requests.Cpu())
				require.Equal(t, resource.MustParse("128Mi"), *resources.Requests.Memory())
				require.Equal(t, resource.MustParse("200m"), *resources.Limits.Cpu())
				require.Equal(t, resource.MustParse("256Mi"), *resources.Limits.Memory())
			},
		},
		{
			name: "applies tolerations from deployment config",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "test-deployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{Name: "manager"},
										},
									},
								},
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "test-ns",
				TargetNamespaces: []string{"test-ns"},
				DeploymentConfig: &config.DeploymentConfig{
					Tolerations: []corev1.Toleration{
						{
							Key:      "node.kubernetes.io/disk-type",
							Operator: corev1.TolerationOpEqual,
							Value:    "ssd",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			verify: func(t *testing.T, objs []client.Object) {
				require.Len(t, objs, 1)
				dep := objs[0].(*appsv1.Deployment)
				tolerations := dep.Spec.Template.Spec.Tolerations

				require.Len(t, tolerations, 1)
				require.Equal(t, "node.kubernetes.io/disk-type", tolerations[0].Key)
				require.Equal(t, corev1.TolerationOpEqual, tolerations[0].Operator)
				require.Equal(t, "ssd", tolerations[0].Value)
				require.Equal(t, corev1.TaintEffectNoSchedule, tolerations[0].Effect)
			},
		},
		{
			name: "applies node selector from deployment config",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "test-deployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{Name: "manager"},
										},
										NodeSelector: map[string]string{
											"existing-key": "existing-value",
										},
									},
								},
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "test-ns",
				TargetNamespaces: []string{"test-ns"},
				DeploymentConfig: &config.DeploymentConfig{
					NodeSelector: map[string]string{
						"disk-type": "ssd",
					},
				},
			},
			verify: func(t *testing.T, objs []client.Object) {
				require.Len(t, objs, 1)
				dep := objs[0].(*appsv1.Deployment)

				// Node selector should be replaced, not merged
				require.Equal(t, map[string]string{"disk-type": "ssd"}, dep.Spec.Template.Spec.NodeSelector)
			},
		},
		{
			name: "applies affinity from deployment config",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "test-deployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{Name: "manager"},
										},
									},
								},
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "test-ns",
				TargetNamespaces: []string{"test-ns"},
				DeploymentConfig: &config.DeploymentConfig{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "kubernetes.io/arch",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"amd64", "arm64"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			verify: func(t *testing.T, objs []client.Object) {
				require.Len(t, objs, 1)
				dep := objs[0].(*appsv1.Deployment)

				require.NotNil(t, dep.Spec.Template.Spec.Affinity)
				require.NotNil(t, dep.Spec.Template.Spec.Affinity.NodeAffinity)
				require.NotNil(t, dep.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
				require.Len(t, dep.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms, 1)
			},
		},
		{
			name: "applies annotations from deployment config",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithAnnotations(map[string]string{
						"csv-annotation": "csv-value",
					}).
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "test-deployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									ObjectMeta: metav1.ObjectMeta{
										Annotations: map[string]string{
											"existing-pod-annotation": "existing-pod-value",
										},
									},
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{Name: "manager"},
										},
									},
								},
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "test-ns",
				TargetNamespaces: []string{"test-ns"},
				DeploymentConfig: &config.DeploymentConfig{
					Annotations: map[string]string{
						"config-annotation":       "config-value",
						"existing-pod-annotation": "should-not-override",
					},
				},
			},
			verify: func(t *testing.T, objs []client.Object) {
				require.Len(t, objs, 1)
				dep := objs[0].(*appsv1.Deployment)

				// Deployment annotations should include config annotations
				// (CSV annotations are only merged into pod template by the generator)
				require.Contains(t, dep.Annotations, "config-annotation")
				require.Equal(t, "config-value", dep.Annotations["config-annotation"])

				// Pod template annotations should include CSV annotations (merged by generator)
				// and existing pod annotations should take precedence over config
				require.Contains(t, dep.Spec.Template.Annotations, "csv-annotation")
				require.Equal(t, "csv-value", dep.Spec.Template.Annotations["csv-annotation"])
				require.Contains(t, dep.Spec.Template.Annotations, "existing-pod-annotation")
				require.Equal(t, "existing-pod-value", dep.Spec.Template.Annotations["existing-pod-annotation"])
				require.Contains(t, dep.Spec.Template.Annotations, "config-annotation")
				require.Equal(t, "config-value", dep.Spec.Template.Annotations["config-annotation"])
			},
		},
		{
			name: "applies volumes and volume mounts from deployment config",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "test-deployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{Name: "manager"},
										},
									},
								},
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "test-ns",
				TargetNamespaces: []string{"test-ns"},
				DeploymentConfig: &config.DeploymentConfig{
					Volumes: []corev1.Volume{
						{
							Name: "config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "my-config"},
								},
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "config-volume",
							MountPath: "/etc/config",
						},
					},
				},
			},
			verify: func(t *testing.T, objs []client.Object) {
				require.Len(t, objs, 1)
				dep := objs[0].(*appsv1.Deployment)

				// Check volume was added
				require.Len(t, dep.Spec.Template.Spec.Volumes, 1)
				require.Equal(t, "config-volume", dep.Spec.Template.Spec.Volumes[0].Name)

				// Check volume mount was added to container
				require.Len(t, dep.Spec.Template.Spec.Containers[0].VolumeMounts, 1)
				require.Equal(t, "config-volume", dep.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name)
				require.Equal(t, "/etc/config", dep.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath)
			},
		},
		{
			name: "applies envFrom from deployment config",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "test-deployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{Name: "manager"},
										},
									},
								},
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "test-ns",
				TargetNamespaces: []string{"test-ns"},
				DeploymentConfig: &config.DeploymentConfig{
					EnvFrom: []corev1.EnvFromSource{
						{
							ConfigMapRef: &corev1.ConfigMapEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "env-config"},
							},
						},
						{
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "env-secret"},
							},
						},
					},
				},
			},
			verify: func(t *testing.T, objs []client.Object) {
				require.Len(t, objs, 1)
				dep := objs[0].(*appsv1.Deployment)

				envFrom := dep.Spec.Template.Spec.Containers[0].EnvFrom
				require.Len(t, envFrom, 2)

				// Check ConfigMap ref
				require.NotNil(t, envFrom[0].ConfigMapRef)
				require.Equal(t, "env-config", envFrom[0].ConfigMapRef.Name)

				// Check Secret ref
				require.NotNil(t, envFrom[1].SecretRef)
				require.Equal(t, "env-secret", envFrom[1].SecretRef.Name)
			},
		},
		{
			name: "applies all config fields together",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "test-deployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{Name: "manager"},
										},
									},
								},
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "test-ns",
				TargetNamespaces: []string{"test-ns"},
				DeploymentConfig: &config.DeploymentConfig{
					Env: []corev1.EnvVar{
						{Name: "ENV_VAR", Value: "value"},
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
					},
					Tolerations: []corev1.Toleration{
						{Key: "key1", Operator: corev1.TolerationOpEqual, Value: "value1"},
					},
					NodeSelector: map[string]string{
						"disk": "ssd",
					},
					Annotations: map[string]string{
						"annotation-key": "annotation-value",
					},
				},
			},
			verify: func(t *testing.T, objs []client.Object) {
				require.Len(t, objs, 1)
				dep := objs[0].(*appsv1.Deployment)

				// Verify env was applied
				require.Len(t, dep.Spec.Template.Spec.Containers[0].Env, 1)
				require.Equal(t, "ENV_VAR", dep.Spec.Template.Spec.Containers[0].Env[0].Name)

				// Verify resources were applied
				require.NotNil(t, dep.Spec.Template.Spec.Containers[0].Resources.Requests)

				// Verify tolerations were applied
				require.Len(t, dep.Spec.Template.Spec.Tolerations, 1)

				// Verify node selector was applied
				require.Equal(t, map[string]string{"disk": "ssd"}, dep.Spec.Template.Spec.NodeSelector)

				// Verify annotations were applied
				require.Contains(t, dep.Annotations, "annotation-key")
				require.Contains(t, dep.Spec.Template.Annotations, "annotation-key")
			},
		},
		{
			name: "applies config to multiple containers",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "test-deployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{Name: "container1"},
											{Name: "container2"},
											{Name: "container3"},
										},
									},
								},
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "test-ns",
				TargetNamespaces: []string{"test-ns"},
				DeploymentConfig: &config.DeploymentConfig{
					Env: []corev1.EnvVar{
						{Name: "SHARED_VAR", Value: "shared_value"},
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
					},
				},
			},
			verify: func(t *testing.T, objs []client.Object) {
				require.Len(t, objs, 1)
				dep := objs[0].(*appsv1.Deployment)

				// All containers should have the env var
				for i := range dep.Spec.Template.Spec.Containers {
					container := dep.Spec.Template.Spec.Containers[i]
					require.Len(t, container.Env, 1)
					require.Equal(t, "SHARED_VAR", container.Env[0].Name)
					require.Equal(t, "shared_value", container.Env[0].Value)

					// All containers should have the resources
					require.NotNil(t, container.Resources.Requests)
					require.Equal(t, resource.MustParse("100m"), *container.Resources.Requests.Cpu())
				}
			},
		},
		{
			name: "nil deployment config does nothing",
			bundle: &bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().
					WithStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{
							Name: "test-deployment",
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name: "manager",
												Env: []corev1.EnvVar{
													{Name: "EXISTING_VAR", Value: "existing_value"},
												},
											},
										},
									},
								},
							},
						},
					).Build(),
			},
			opts: render.Options{
				InstallNamespace: "test-ns",
				TargetNamespaces: []string{"test-ns"},
				DeploymentConfig: nil,
			},
			verify: func(t *testing.T, objs []client.Object) {
				require.Len(t, objs, 1)
				dep := objs[0].(*appsv1.Deployment)

				// Should only have the existing env var
				require.Len(t, dep.Spec.Template.Spec.Containers[0].Env, 1)
				require.Equal(t, "EXISTING_VAR", dep.Spec.Template.Spec.Containers[0].Env[0].Name)
				require.Equal(t, "existing_value", dep.Spec.Template.Spec.Containers[0].Env[0].Value)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objs, err := generators.BundleCSVDeploymentGenerator(tc.bundle, tc.opts)
			require.NoError(t, err)
			tc.verify(t, objs)
		})
	}
}
