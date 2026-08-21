package generators_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/generators"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
	"github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	mockrender "github.com/operator-framework/operator-controller/internal/testutil/mock/render"
)

func Test_CertMutator_FailsOnNil(t *testing.T) {
	ctx := render.NewContext(nil, render.Options{})
	err := generators.CertMutator(ctx)
	objs := ctx.Objects
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle cannot be nil")
}

// Test_CertMutator_DeploymentCertVolumes_Succeeds asserts that CertMutator rewrites the certificate
// volumes/mounts of a webhook-serving deployment produced by BundleCSVDeploymentGenerator.
func Test_CertMutator_DeploymentCertVolumes_Succeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	fakeProvider := mockrender.NewMockCertificateProvider(ctrl)
	fakeProvider.EXPECT().GetCertSecretInfo(gomock.Any()).Return(render.CertSecretInfo{
		SecretName:     "some-secret",
		CertificateKey: "some-cert-key",
		PrivateKeyKey:  "some-private-key-key",
	}).AnyTimes()
	fakeProvider.EXPECT().InjectCABundle(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	fakeProvider.EXPECT().AdditionalObjects(gomock.Any()).Return(nil, nil).AnyTimes()

	b := &bundle.RegistryV1{
		CSV: csv.Builder().
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

	ctx := render.NewContext(b, render.Options{
		InstallNamespace:    "install-namespace",
		CertificateProvider: fakeProvider,
	})
	require.NoError(t, generators.BundleCSVDeploymentGenerator(ctx))
	require.NoError(t, generators.CertMutator(ctx))
	objs := ctx.Objects

	var deployment *appsv1.Deployment
	for _, obj := range objs {
		if d, ok := obj.(*appsv1.Deployment); ok {
			deployment = d
			break
		}
	}
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

// Test_CertMutator_CRDConversionWebhook_Succeeds asserts that CertMutator sets spec.conversion on
// CRDs referenced by a conversion webhook in the bundle's CSV.
func Test_CertMutator_CRDConversionWebhook_Succeeds(t *testing.T) {
	opts := render.Options{
		InstallNamespace: "install-namespace",
		TargetNamespaces: []string{""},
	}

	b := &bundle.RegistryV1{
		CRDs: []apiextensionsv1.CustomResourceDefinition{
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-one"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-two"}},
		},
		CSV: csv.Builder().
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

	ctx := render.NewContext(b, opts)
	require.NoError(t, generators.BundleCRDGenerator(ctx))
	require.NoError(t, generators.CertMutator(ctx))
	objs := ctx.Objects
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

// Test_CertMutator_CRDConversionWebhook_PreserveUnknownFieldsFails asserts that CertMutator rejects
// CRDs referenced by a conversion webhook that have spec.preserveUnknownFields set to true.
func Test_CertMutator_CRDConversionWebhook_PreserveUnknownFieldsFails(t *testing.T) {
	opts := render.Options{
		InstallNamespace: "install-namespace",
		TargetNamespaces: []string{""},
	}

	b := &bundle.RegistryV1{
		CRDs: []apiextensionsv1.CustomResourceDefinition{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "crd-one"},
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					PreserveUnknownFields: true,
				},
			},
		},
		CSV: csv.Builder().
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

	ctx := render.NewContext(b, opts)
	require.NoError(t, generators.BundleCRDGenerator(ctx))
	err := generators.CertMutator(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must have .spec.preserveUnknownFields set to false to let API Server call webhook to do the conversion")
}

// Test_CertMutator_CRDConversionWebhook_WithCertProvider_Succeeds asserts that CertMutator injects
// the CA bundle into CRDs referenced by a conversion webhook when a CertificateProvider is configured.
func Test_CertMutator_CRDConversionWebhook_WithCertProvider_Succeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	fakeProvider := mockrender.NewMockCertificateProvider(ctrl)
	fakeProvider.EXPECT().InjectCABundle(gomock.Any(), gomock.Any()).DoAndReturn(func(obj client.Object, _ render.CertificateProvisionerConfig) error {
		obj.SetAnnotations(map[string]string{
			"cert-provider": "annotation",
		})
		return nil
	}).AnyTimes()
	fakeProvider.EXPECT().AdditionalObjects(gomock.Any()).Return(nil, nil).AnyTimes()

	opts := render.Options{
		InstallNamespace:    "install-namespace",
		TargetNamespaces:    []string{""},
		CertificateProvider: fakeProvider,
	}

	b := &bundle.RegistryV1{
		CRDs: []apiextensionsv1.CustomResourceDefinition{
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-one"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "crd-two"}},
		},
		CSV: csv.Builder().
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

	ctx := render.NewContext(b, opts)
	require.NoError(t, generators.BundleCRDGenerator(ctx))
	require.NoError(t, generators.CertMutator(ctx))
	objs := ctx.Objects
	require.Len(t, objs, 2)
	require.Equal(t, map[string]string{
		"cert-provider": "annotation",
	}, objs[0].GetAnnotations())
}

// Test_CertMutator_ValidatingWebhookClientConfig_Succeeds asserts that CertMutator sets the
// ClientConfig.Service reference on ValidatingWebhookConfigurations produced by the base generator.
func Test_CertMutator_ValidatingWebhookClientConfig_Succeeds(t *testing.T) {
	for _, tc := range []struct {
		name              string
		bundle            *bundle.RegistryV1
		opts              render.Options
		expectedResources []client.Object
	}{
		{
			name: "generates validating webhook configuration resources described in the bundle's cluster service version",
			bundle: &bundle.RegistryV1{
				CSV: csv.Builder().
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
				CSV: csv.Builder().
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := render.NewContext(tc.bundle, tc.opts)
			require.NoError(t, generators.BundleValidatingWebhookResourceGenerator(ctx))
			require.NoError(t, generators.CertMutator(ctx))
			objs := ctx.Objects
			require.Equal(t, tc.expectedResources, objs)
		})
	}
}

// Test_CertMutator_ValidatingWebhookConfig_WithCertProvider_Succeeds asserts that CertMutator sets
// the ClientConfig.Service reference and injects the CA bundle when a CertificateProvider is configured.
func Test_CertMutator_ValidatingWebhookConfig_WithCertProvider_Succeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	fakeProvider := mockrender.NewMockCertificateProvider(ctrl)
	fakeProvider.EXPECT().InjectCABundle(gomock.Any(), gomock.Any()).DoAndReturn(func(obj client.Object, _ render.CertificateProvisionerConfig) error {
		obj.SetAnnotations(map[string]string{
			"cert-provider": "annotation",
		})
		return nil
	}).AnyTimes()
	fakeProvider.EXPECT().AdditionalObjects(gomock.Any()).Return(nil, nil).AnyTimes()

	b := &bundle.RegistryV1{
		CSV: csv.Builder().
			WithWebhookDefinitions(
				v1alpha1.WebhookDescription{
					Type:           v1alpha1.ValidatingAdmissionWebhook,
					GenerateName:   "my-webhook",
					DeploymentName: "my-deployment",
					ContainerPort:  443,
				},
			).Build(),
	}
	opts := render.Options{
		InstallNamespace:    "install-namespace",
		TargetNamespaces:    []string{"watch-namespace-one", "watch-namespace-two"},
		CertificateProvider: fakeProvider,
	}

	ctx := render.NewContext(b, opts)
	require.NoError(t, generators.BundleValidatingWebhookResourceGenerator(ctx))
	require.NoError(t, generators.CertMutator(ctx))
	objs := ctx.Objects
	require.Equal(t, []client.Object{
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
	}, objs)
}

// Test_CertMutator_MutatingWebhookClientConfig_Succeeds asserts that CertMutator sets the
// ClientConfig.Service reference on MutatingWebhookConfigurations produced by the base generator.
func Test_CertMutator_MutatingWebhookClientConfig_Succeeds(t *testing.T) {
	for _, tc := range []struct {
		name              string
		bundle            *bundle.RegistryV1
		opts              render.Options
		expectedResources []client.Object
	}{
		{
			name: "generates mutating webhook configuration resources described in the bundle's cluster service version",
			bundle: &bundle.RegistryV1{
				CSV: csv.Builder().
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
				CSV: csv.Builder().
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := render.NewContext(tc.bundle, tc.opts)
			require.NoError(t, generators.BundleMutatingWebhookResourceGenerator(ctx))
			require.NoError(t, generators.CertMutator(ctx))
			objs := ctx.Objects
			require.Equal(t, tc.expectedResources, objs)
		})
	}
}

// Test_CertMutator_MutatingWebhookConfig_WithCertProvider_Succeeds asserts that CertMutator sets
// the ClientConfig.Service reference and injects the CA bundle when a CertificateProvider is configured.
func Test_CertMutator_MutatingWebhookConfig_WithCertProvider_Succeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	fakeProvider := mockrender.NewMockCertificateProvider(ctrl)
	fakeProvider.EXPECT().InjectCABundle(gomock.Any(), gomock.Any()).DoAndReturn(func(obj client.Object, _ render.CertificateProvisionerConfig) error {
		obj.SetAnnotations(map[string]string{
			"cert-provider": "annotation",
		})
		return nil
	}).AnyTimes()
	fakeProvider.EXPECT().AdditionalObjects(gomock.Any()).Return(nil, nil).AnyTimes()

	b := &bundle.RegistryV1{
		CSV: csv.Builder().
			WithWebhookDefinitions(
				v1alpha1.WebhookDescription{
					Type:           v1alpha1.MutatingAdmissionWebhook,
					GenerateName:   "my-webhook",
					DeploymentName: "my-deployment",
					ContainerPort:  443,
				},
			).Build(),
	}
	opts := render.Options{
		InstallNamespace:    "install-namespace",
		TargetNamespaces:    []string{"watch-namespace-one", "watch-namespace-two"},
		CertificateProvider: fakeProvider,
	}

	ctx := render.NewContext(b, opts)
	require.NoError(t, generators.BundleMutatingWebhookResourceGenerator(ctx))
	require.NoError(t, generators.CertMutator(ctx))
	objs := ctx.Objects
	require.Equal(t, []client.Object{
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
	}, objs)
}

// Test_CertMutator_WebhookService_Succeeds asserts that CertMutator creates the webhook-serving
// Service(s) backing the bundle's webhook definitions.
func Test_CertMutator_WebhookService_Succeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	fakeProvider := mockrender.NewMockCertificateProvider(ctrl)
	fakeProvider.EXPECT().InjectCABundle(gomock.Any(), gomock.Any()).DoAndReturn(func(obj client.Object, _ render.CertificateProvisionerConfig) error {
		obj.SetAnnotations(map[string]string{
			"cert-provider": "annotation",
		})
		return nil
	}).AnyTimes()
	fakeProvider.EXPECT().AdditionalObjects(gomock.Any()).Return(nil, nil).AnyTimes()

	for _, tc := range []struct {
		name              string
		bundle            *bundle.RegistryV1
		opts              render.Options
		expectedResources []client.Object
	}{
		{
			name: "generates webhook services using container port 443 and target port 443 by default",
			bundle: &bundle.RegistryV1{
				CSV: csv.Builder().
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
				CSV: csv.Builder().
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
				CSV: csv.Builder().
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
				CSV: csv.Builder().
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
				CSV: csv.Builder().
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
				CSV: csv.Builder().
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
				CSV: csv.Builder().
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
			ctx := render.NewContext(tc.bundle, tc.opts)
			require.NoError(t, generators.CertMutator(ctx))
			objs := ctx.Objects
			require.Equal(t, tc.expectedResources, objs)
		})
	}
}

// Test_CertMutator_AdditionalObjects_Succeeds asserts that CertMutator appends the additional
// provider objects (e.g. Issuer/Certificate) for each deployment referenced by a webhook definition,
// and appends them after the webhook-serving Service(s).
func Test_CertMutator_AdditionalObjects_Succeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	fakeProvider := mockrender.NewMockCertificateProvider(ctrl)
	fakeProvider.EXPECT().InjectCABundle(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	fakeProvider.EXPECT().AdditionalObjects(gomock.Any()).DoAndReturn(func(cfg render.CertificateProvisionerConfig) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{*ToUnstructuredT(t, &corev1.Secret{
			TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: corev1.SchemeGroupVersion.String()},
			ObjectMeta: metav1.ObjectMeta{
				Name: cfg.CertName,
			},
		})}, nil
	}).AnyTimes()

	ctx := render.NewContext(&bundle.RegistryV1{
		CSV: csv.Builder().
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
	require.NoError(t, generators.CertMutator(ctx))
	objs := ctx.Objects
	require.Equal(t, []client.Object{
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
		ToUnstructuredT(t, &corev1.Secret{
			TypeMeta:   metav1.TypeMeta{Kind: "Secret", APIVersion: corev1.SchemeGroupVersion.String()},
			ObjectMeta: metav1.ObjectMeta{Name: "my-deployment-service-cert"},
		}),
	}, objs)
}
