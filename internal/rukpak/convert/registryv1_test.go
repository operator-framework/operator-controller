package convert

import (
	"context"
	"fmt"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

func TestRegistryV1ToHelmChart(t *testing.T) {
	type testCase struct {
		name             string
		cfg              genBundleConfig
		installNamespace string
		convertOptions   []ToHelmChartOption
		assert           func(*testing.T, string, *chart.Chart, error, string, error)
	}

	type configOverrideFields struct {
		volumes      []corev1.Volume
		volumeMounts []corev1.VolumeMount
	}

	allNamespacesAssertions := func(t *testing.T, installNamespace string, _ *chart.Chart, _ error, manifest string, _ error) {
		// The deployment's pod metadata should have CSV annotations + olm.targetNamespaces=""
		expectedProperties := mustJSONMarshal(append(csvAnnotationsProperties(), bundleProperties()...))
		expectedAnnotations := mergeMaps(
			csvAnnotations(),
			map[string]string{"olm.properties": string(expectedProperties)},
			map[string]string{"olm.targetNamespaces": ""},
		)
		assertInManifest(t, manifest,
			func(obj client.Object) (bool, error) {
				return obj.GetObjectKind().GroupVersionKind().Kind == "Deployment", nil
			},
			func(t *testing.T, obj client.Object) {
				assertFieldEqual(t, obj, expectedAnnotations, `{.spec.template.metadata.annotations}`)
			},
		)

		// operators watching all namespaces should have their permissions promoted to a ClusterRole and ClusterRoleBinding
		clusterRoleName := ""
		assertPresent(t, manifest,
			func(obj client.Object) (bool, error) {
				if obj.GetObjectKind().GroupVersionKind().Kind != "ClusterRole" {
					return false, nil
				}
				var cr rbacv1.ClusterRole
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, &cr); err != nil {
					return false, nil
				}
				if !equality.Semantic.DeepEqualWithNilDifferentFromEmpty(csvPermissionRules(), cr.Rules) {
					return false, nil
				}
				clusterRoleName = cr.Name
				return true, nil
			},
		)
		assertInManifest(t, manifest,
			func(obj client.Object) (bool, error) {
				if obj.GetObjectKind().GroupVersionKind().Kind != "ClusterRoleBinding" {
					return false, nil
				}
				var rb rbacv1.RoleBinding
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, &rb); err != nil {
					return false, err
				}
				return rb.RoleRef.Name == clusterRoleName, nil
			},
			func(t *testing.T, obj client.Object) {
				subjects := []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      csvPodServiceAccountName(),
						Namespace: installNamespace,
					},
				}
				assertFieldEqual(t, obj, subjects, `{.subjects}`)
			},
		)
		assertNotPresent(t, manifest,
			func(obj client.Object) (bool, error) {
				// There should be no Roles in the manifest
				return obj.GetObjectKind().GroupVersionKind().Kind == "Role", nil
			},
		)
	}

	standardAssertions := func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error, overrides configOverrideFields) {
		expectedProperties := mustJSONMarshal(append(csvAnnotationsProperties(), bundleProperties()...))
		expectedAnnotations := mergeMaps(csvAnnotations(), map[string]string{"olm.properties": string(expectedProperties)})

		assert.NoError(t, convertError) //nolint:testifylint
		assert.NotNil(t, chrt)
		assert.NoError(t, templateError) //nolint:testifylint
		assert.NotEmpty(t, manifest)

		assert.Equal(t, "v2", chrt.Metadata.APIVersion)
		assert.Equal(t, "example-operator", chrt.Metadata.Name)
		assert.Equal(t, csvSpecVersion().String(), chrt.Metadata.Version)
		assert.Equal(t, csvSpecDescription(), chrt.Metadata.Description)
		assert.Equal(t, csvSpecKeywords(), chrt.Metadata.Keywords)
		assert.Equal(t, convertMaintainers(csvSpecMaintainers()), chrt.Metadata.Maintainers)
		assert.Equal(t, expectedAnnotations, chrt.Metadata.Annotations)
		assert.Equal(t, convertSpecLinks(csvSpecLinks()), chrt.Metadata.Sources)
		assert.Equal(t, csvSpecProvider().URL, chrt.Metadata.Home)
		assert.Equal(t, ">= "+csvSpecMinKubeVersion(), chrt.Metadata.KubeVersion)

		assertInManifest(t, manifest,
			func(obj client.Object) (bool, error) {
				return obj.GetObjectKind().GroupVersionKind().Kind == "Deployment", nil
			},
			func(t *testing.T, obj client.Object) {
				assertFieldEqual(t, obj, csvDeploymentSelector(), `{.spec.selector}`)
				assertFieldEqual(t, obj, csvPodNodeSelector(), `{.spec.template.spec.nodeSelector}`)
				assertFieldEqual(t, obj, csvPodTolerations(), `{.spec.template.spec.tolerations}`)
				assertFieldEqual(t, obj, csvPodAffinity(), `{.spec.template.spec.affinity}`)
				assertFieldEqual(t, obj, csvContainerResources(), `{.spec.template.spec.containers[0].resources}`)
				assertFieldEqual(t, obj, csvContainerEnv(), `{.spec.template.spec.containers[0].env}`)
				assertFieldEqual(t, obj, csvContainerEnvFrom(), `{.spec.template.spec.containers[0].envFrom}`)
				assertFieldEqual(t, obj, csvPodLabels(), `{.spec.template.metadata.labels}`)
				assertFieldEqual(t, obj, csvContainerName(), `{.spec.template.spec.containers[0].name}`)
				assertFieldEqual(t, obj, csvContainerImage(), `{.spec.template.spec.containers[0].image}`)

				if overrides.volumes != nil {
					assertFieldEqual(t, obj, overrides.volumes, `{.spec.template.spec.volumes}`)
				} else {
					assertFieldEqual(t, obj, csvPodVolumes(), `{.spec.template.spec.volumes}`)
				}

				if overrides.volumeMounts != nil {
					assertFieldEqual(t, obj, overrides.volumeMounts, `{.spec.template.spec.containers[0].volumeMounts}`)
				} else {
					assertFieldEqual(t, obj, csvContainerVolumeMounts(), `{.spec.template.spec.containers[0].volumeMounts}`)
				}
			},
		)

		assertPresent(t, manifest,
			func(obj client.Object) (bool, error) {
				if obj.GetObjectKind().GroupVersionKind().Kind != "ClusterRole" {
					return false, nil
				}
				var cr rbacv1.ClusterRole
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, &cr); err != nil {
					return false, err
				}
				return equality.Semantic.DeepEqualWithNilDifferentFromEmpty(csvClusterPermissionRules(), cr.Rules), nil
			},
		)

		assertPresent(t, manifest,
			func(obj client.Object) (bool, error) {
				return obj.GetObjectKind().GroupVersionKind().Kind == "ServiceAccount" &&
					obj.GetName() == csvPodServiceAccountName() &&
					(obj.GetNamespace() == installNamespace || obj.GetNamespace() == ""), nil
			},
		)

		assertInManifest(t, manifest,
			func(obj client.Object) (bool, error) {
				return obj.GetObjectKind().GroupVersionKind().Kind == "ConfigMap" && obj.GetName() == "example-config", nil
			},
			func(t *testing.T, obj client.Object) {
				assertFieldEqual(t, obj, configMapData(), `{.data}`)
			},
		)

		assertInManifest(t, manifest,
			func(obj client.Object) (bool, error) {
				return obj.GetObjectKind().GroupVersionKind().Kind == "CustomResourceDefinition" && obj.GetName() == crdName(), nil
			},
			func(t *testing.T, obj client.Object) {
				assertFieldEqual(t, obj, crdSpec().Group, `{.spec.group}`)
				assertFieldEqual(t, obj, crdSpec().Names, `{.spec.names}`)
				assertFieldEqual(t, obj, crdSpec().Scope, `{.spec.scope}`)
				assertFieldEqual(t, obj, crdSpec().Versions, `{.spec.versions}`)
			},
		)
	}

	tests := []testCase{
		{
			name: "No overrides",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces|OwnNamespace",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces|SingleNamespace",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace},
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces|MultiNamespace",
			cfg: genBundleConfig{
				supportedInstallModes: []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeMultiNamespace},
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, configOverrideFields{})
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces, no cert provider, with conversion webhook",
			cfg: genBundleConfig{
				supportedInstallModes:    []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
				includeConversionWebhook: true,
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.ErrorContains(t, convertError, "webhook definitions are not supported when a certificate provider is not configured")
			},
		},
		{
			name: "AllNamespaces, include all webhooks, cert-manager",
			cfg: genBundleConfig{
				supportedInstallModes:    []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
				includeConversionWebhook: true,
				includeValidatingWebhook: true,
				includeMutatingWebhook:   true,
			},
			installNamespace: "test-namespace",
			convertOptions:   []ToHelmChartOption{WithCertManagerCertificateProvider()},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.NoError(t, convertError) //nolint:testifylint
				assert.NotNil(t, chrt)
				assert.NoError(t, templateError) //nolint:testifylint
				assert.NotEmpty(t, manifest)

				cwDef := csvWebhookConversionDefinition()
				vwDef := csvWebhookValidationDefinition()
				mwDef := csvWebhookMutationDefinition()

				var svcNames []string
				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "Service", nil
					},
					func(t *testing.T, obj client.Object) {
						svcNames = append(svcNames, obj.GetName())
						expectedSpec := corev1.ServiceSpec{
							Ports: []corev1.ServicePort{
								{Name: fmt.Sprintf("%d", cwDef.ContainerPort), Port: cwDef.ContainerPort, TargetPort: *cwDef.TargetPort},
								{Name: fmt.Sprintf("%d", vwDef.ContainerPort), Port: vwDef.ContainerPort, TargetPort: *vwDef.TargetPort},
								{Name: fmt.Sprintf("%d", mwDef.ContainerPort), Port: mwDef.ContainerPort, TargetPort: *mwDef.TargetPort},
							},
							Selector: csvPodLabels(),
						}
						assertFieldEqual(t, obj, expectedSpec, `{.spec}`)
					},
				)
				require.Len(t, svcNames, 1)

				var issuerNames []string

				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "Issuer", nil
					},
					func(t *testing.T, obj client.Object) {
						issuerNames = append(issuerNames, obj.GetName())
						expectedSpec := certmanagerv1.IssuerConfig{
							SelfSigned: &certmanagerv1.SelfSignedIssuer{},
						}
						assertFieldEqual(t, obj, expectedSpec, `{.spec}`)
					},
				)
				require.Len(t, issuerNames, 1)

				var secretNames []string
				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "Certificate", nil
					},
					func(t *testing.T, obj client.Object) {
						var cert certmanagerv1.Certificate
						if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, &cert); err != nil {
							t.Fatal(err)
						}
						secretNames = append(secretNames, cert.Spec.SecretName)
						assertFieldEqual(t, obj, []string{fmt.Sprintf("%s.%s.svc", svcNames[0], installNamespace)}, `{.spec.dnsNames}`)
						assertFieldEqual(t, obj, []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth}, `{.spec.usages}`)
						assertFieldEqual(t, obj, certmanagermetav1.ObjectReference{Name: issuerNames[0]}, `{.spec.issuerRef}`)
					},
				)
				require.Len(t, secretNames, 1)

				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "CustomResourceDefinition", nil
					},
					func(t *testing.T, obj client.Object) {
						conversion := apiextensionsv1.CustomResourceConversion{
							Strategy: apiextensionsv1.WebhookConverter,
							Webhook: &apiextensionsv1.WebhookConversion{
								ClientConfig: &apiextensionsv1.WebhookClientConfig{Service: &apiextensionsv1.ServiceReference{
									Name: svcNames[0], Namespace: installNamespace, Path: cwDef.WebhookPath, Port: ptr.To(cwDef.ContainerPort),
								}},
								ConversionReviewVersions: cwDef.AdmissionReviewVersions,
							},
						}
						assertFieldEqual(t, obj, conversion, `{.spec.conversion}`)
					},
				)

				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "ValidatingWebhookConfiguration", nil
					},
					func(t *testing.T, obj client.Object) {
						validation := []admissionregistrationv1.ValidatingWebhook{{
							AdmissionReviewVersions: vwDef.AdmissionReviewVersions,
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Name: svcNames[0], Namespace: installNamespace, Path: vwDef.WebhookPath, Port: ptr.To(vwDef.ContainerPort),
								},
							},
							FailurePolicy:  vwDef.FailurePolicy,
							MatchPolicy:    vwDef.MatchPolicy,
							Name:           vwDef.GenerateName,
							ObjectSelector: vwDef.ObjectSelector,
							Rules:          vwDef.Rules,
							SideEffects:    vwDef.SideEffects,
							TimeoutSeconds: vwDef.TimeoutSeconds,
						}}
						assertFieldEqual(t, obj, validation, `{.webhooks}`)
						assertFieldEqual(t, obj, map[string]string{"cert-manager.io/inject-ca-from": fmt.Sprintf("%s/%s", installNamespace, csvDeploymentName())}, `{.metadata.annotations}`)
					},
				)
				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "MutatingWebhookConfiguration", nil
					},
					func(t *testing.T, obj client.Object) {
						mutation := []admissionregistrationv1.MutatingWebhook{{
							AdmissionReviewVersions: mwDef.AdmissionReviewVersions,
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Name: svcNames[0], Namespace: installNamespace, Path: mwDef.WebhookPath, Port: ptr.To(mwDef.ContainerPort),
								},
							},
							FailurePolicy:      mwDef.FailurePolicy,
							MatchPolicy:        mwDef.MatchPolicy,
							Name:               mwDef.GenerateName,
							ObjectSelector:     mwDef.ObjectSelector,
							ReinvocationPolicy: mwDef.ReinvocationPolicy,
							Rules:              mwDef.Rules,
							SideEffects:        mwDef.SideEffects,
							TimeoutSeconds:     mwDef.TimeoutSeconds,
						}}
						assertFieldEqual(t, obj, mutation, `{.webhooks}`)
						assertFieldEqual(t, obj, map[string]string{"cert-manager.io/inject-ca-from": fmt.Sprintf("%s/%s", installNamespace, csvDeploymentName())}, `{.metadata.annotations}`)
					},
				)

				certificateProviderOverrides := configOverrideFields{
					volumes: append(csvPodVolumes(),
						corev1.Volume{Name: "apiservice-cert", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: fmt.Sprintf("%s-%s-cert", csvName(), csvDeploymentName()), Items: []corev1.KeyToPath{
							{Key: "tls.crt", Path: "apiserver.crt"},
							{Key: "tls.key", Path: "apiserver.key"},
						}}}},
						corev1.Volume{Name: "webhook-cert", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: fmt.Sprintf("%s-%s-cert", csvName(), csvDeploymentName()), Items: []corev1.KeyToPath{
							{Key: "tls.crt", Path: "tls.crt"},
							{Key: "tls.key", Path: "tls.key"},
						}}}},
					),
					volumeMounts: append(csvContainerVolumeMounts(),
						corev1.VolumeMount{Name: "apiservice-cert", MountPath: "/apiserver.local.config/certificates"},
						corev1.VolumeMount{Name: "webhook-cert", MountPath: "/tmp/k8s-webhook-server/serving-certs"},
					),
				}

				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, certificateProviderOverrides)
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces, include all webhooks, openshift-service-ca",
			cfg: genBundleConfig{
				supportedInstallModes:    []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
				includeConversionWebhook: true,
				includeValidatingWebhook: true,
				includeMutatingWebhook:   true,
			},
			installNamespace: "test-namespace",
			convertOptions:   []ToHelmChartOption{WithOpenShiftServiceCACertificateProvider()},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.NoError(t, convertError) //nolint:testifylint
				assert.NotNil(t, chrt)
				assert.NoError(t, templateError) //nolint:testifylint
				assert.NotEmpty(t, manifest)

				cwDef := csvWebhookConversionDefinition()
				vwDef := csvWebhookValidationDefinition()
				mwDef := csvWebhookMutationDefinition()

				var svcNames []string
				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "Service", nil
					},
					func(t *testing.T, obj client.Object) {
						svcNames = append(svcNames, obj.GetName())
						expectedSpec := corev1.ServiceSpec{
							Ports: []corev1.ServicePort{
								{Name: fmt.Sprintf("%d", cwDef.ContainerPort), Port: cwDef.ContainerPort, TargetPort: *cwDef.TargetPort},
								{Name: fmt.Sprintf("%d", vwDef.ContainerPort), Port: vwDef.ContainerPort, TargetPort: *vwDef.TargetPort},
								{Name: fmt.Sprintf("%d", mwDef.ContainerPort), Port: mwDef.ContainerPort, TargetPort: *mwDef.TargetPort},
							},
							Selector: csvPodLabels(),
						}
						assertFieldEqual(t, obj, expectedSpec, `{.spec}`)

						expectedAnnotations := map[string]string{
							"service.beta.openshift.io/serving-cert-secret-name": fmt.Sprintf("%s-%s-cert", csvName(), csvDeploymentName()),
						}
						assertFieldEqual(t, obj, expectedAnnotations, `{.metadata.annotations}`)
					},
				)
				require.Len(t, svcNames, 1)

				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "CustomResourceDefinition", nil
					},
					func(t *testing.T, obj client.Object) {
						conversion := apiextensionsv1.CustomResourceConversion{
							Strategy: apiextensionsv1.WebhookConverter,
							Webhook: &apiextensionsv1.WebhookConversion{
								ClientConfig: &apiextensionsv1.WebhookClientConfig{Service: &apiextensionsv1.ServiceReference{
									Name: svcNames[0], Namespace: installNamespace, Path: cwDef.WebhookPath, Port: ptr.To(cwDef.ContainerPort),
								}},
								ConversionReviewVersions: cwDef.AdmissionReviewVersions,
							},
						}
						assertFieldEqual(t, obj, conversion, `{.spec.conversion}`)
						assertFieldEqual(t, obj, map[string]string{"service.beta.openshift.io/inject-cabundle": "true"}, `{.metadata.annotations}`)
					},
				)

				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "ValidatingWebhookConfiguration", nil
					},
					func(t *testing.T, obj client.Object) {
						validation := []admissionregistrationv1.ValidatingWebhook{{
							AdmissionReviewVersions: vwDef.AdmissionReviewVersions,
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Name: svcNames[0], Namespace: installNamespace, Path: vwDef.WebhookPath, Port: ptr.To(vwDef.ContainerPort),
								},
							},
							FailurePolicy:  vwDef.FailurePolicy,
							MatchPolicy:    vwDef.MatchPolicy,
							Name:           vwDef.GenerateName,
							ObjectSelector: vwDef.ObjectSelector,
							Rules:          vwDef.Rules,
							SideEffects:    vwDef.SideEffects,
							TimeoutSeconds: vwDef.TimeoutSeconds,
						}}
						assertFieldEqual(t, obj, validation, `{.webhooks}`)
						assertFieldEqual(t, obj, map[string]string{"service.beta.openshift.io/inject-cabundle": "true"}, `{.metadata.annotations}`)
					},
				)
				assertInManifest(t, manifest,
					func(obj client.Object) (bool, error) {
						return obj.GetObjectKind().GroupVersionKind().Kind == "MutatingWebhookConfiguration", nil
					},
					func(t *testing.T, obj client.Object) {
						mutation := []admissionregistrationv1.MutatingWebhook{{
							AdmissionReviewVersions: mwDef.AdmissionReviewVersions,
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Name: svcNames[0], Namespace: installNamespace, Path: mwDef.WebhookPath, Port: ptr.To(mwDef.ContainerPort),
								},
							},
							FailurePolicy:      mwDef.FailurePolicy,
							MatchPolicy:        mwDef.MatchPolicy,
							Name:               mwDef.GenerateName,
							ObjectSelector:     mwDef.ObjectSelector,
							ReinvocationPolicy: mwDef.ReinvocationPolicy,
							Rules:              mwDef.Rules,
							SideEffects:        mwDef.SideEffects,
							TimeoutSeconds:     mwDef.TimeoutSeconds,
						}}
						assertFieldEqual(t, obj, mutation, `{.webhooks}`)
						assertFieldEqual(t, obj, map[string]string{"service.beta.openshift.io/inject-cabundle": "true"}, `{.metadata.annotations}`)
					},
				)

				certificateProviderOverrides := configOverrideFields{
					volumes: append(csvPodVolumes(),
						corev1.Volume{Name: "apiservice-cert", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: fmt.Sprintf("%s-%s-cert", csvName(), csvDeploymentName()), Items: []corev1.KeyToPath{
							{Key: "tls.crt", Path: "apiserver.crt"},
							{Key: "tls.key", Path: "apiserver.key"},
						}}}},
						corev1.Volume{Name: "webhook-cert", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: fmt.Sprintf("%s-%s-cert", csvName(), csvDeploymentName()), Items: []corev1.KeyToPath{
							{Key: "tls.crt", Path: "tls.crt"},
							{Key: "tls.key", Path: "tls.key"},
						}}}},
					),
					volumeMounts: append(csvContainerVolumeMounts(),
						corev1.VolumeMount{Name: "apiservice-cert", MountPath: "/apiserver.local.config/certificates"},
						corev1.VolumeMount{Name: "webhook-cert", MountPath: "/tmp/k8s-webhook-server/serving-certs"},
					),
				}

				standardAssertions(t, installNamespace, chrt, convertError, manifest, templateError, certificateProviderOverrides)
				allNamespacesAssertions(t, installNamespace, chrt, convertError, manifest, templateError)
			},
		},
		{
			name: "AllNamespaces|OwnNamespace, fail with conversion webhook",
			cfg: genBundleConfig{
				supportedInstallModes:    []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
				includeConversionWebhook: true,
			},
			installNamespace: "test-namespace",
			convertOptions:   []ToHelmChartOption{WithCertManagerCertificateProvider()},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.ErrorContains(t, convertError, "CSVs with conversion webhooks must support only AllNamespaces install mode")
			},
		},
		{
			name: "AllNamespaces|OwnNamespace, succeed with admission webhooks",
			cfg: genBundleConfig{
				supportedInstallModes:    []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeOwnNamespace},
				includeValidatingWebhook: true,
				includeMutatingWebhook:   true,
			},
			installNamespace: "test-namespace",
			convertOptions:   []ToHelmChartOption{WithCertManagerCertificateProvider()},
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.NoError(t, convertError) //nolint:testifylint
				assert.NotNil(t, chrt)
				assert.NoError(t, templateError) //nolint:testifylint
				assert.NotEmpty(t, manifest)
			},
		},
		{
			name: "Include API service",
			cfg: genBundleConfig{
				supportedInstallModes:        []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces},
				includeAPIServiceDefinitions: true,
			},
			installNamespace: "test-namespace",
			assert: func(t *testing.T, installNamespace string, chrt *chart.Chart, convertError error, manifest string, templateError error) {
				assert.ErrorContains(t, convertError, "apiServiceDefintions are not supported")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rv1, err := LoadRegistryV1(context.Background(), genBundleFS(tc.cfg))
			require.NoError(t, err)

			gotChart, convertError := rv1.ToHelmChart(tc.convertOptions...)
			if convertError != nil {
				tc.assert(t, tc.installNamespace, gotChart, convertError, "", nil)
				return
			}

			manifest, templateError := templateChart(gotChart, tc.installNamespace, chartutil.Values{})
			tc.assert(t, tc.installNamespace, gotChart, nil, manifest, templateError)
		})
	}
}
