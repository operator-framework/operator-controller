package render

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
)

func BundleWebhookResourceGenerator(rv1 *convert.RegistryV1, opts Options) ([]client.Object, error) {
	//nolint:prealloc
	var objs []client.Object
	webhookServicePortsByDeployment := map[string]sets.Set[corev1.ServicePort]{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		// collect webhook service ports
		if _, ok := webhookServicePortsByDeployment[wh.DeploymentName]; !ok {
			webhookServicePortsByDeployment[wh.DeploymentName] = sets.Set[corev1.ServicePort]{}
		}
		webhookServicePortsByDeployment[wh.DeploymentName].Insert(getWebhookServicePort(wh))

		// collect webhook configurations and crd conversions
		switch wh.Type {
		case v1alpha1.ValidatingAdmissionWebhook:
			objs = append(objs,
				GenerateValidatingWebhookConfigurationResource(
					wh.GenerateName,
					WithValidatingWebhooks(
						admissionregistrationv1.ValidatingWebhook{
							Name:                    wh.GenerateName,
							Rules:                   wh.Rules,
							FailurePolicy:           wh.FailurePolicy,
							MatchPolicy:             wh.MatchPolicy,
							ObjectSelector:          wh.ObjectSelector,
							SideEffects:             wh.SideEffects,
							TimeoutSeconds:          wh.TimeoutSeconds,
							AdmissionReviewVersions: wh.AdmissionReviewVersions,
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Namespace: opts.InstallNamespace,
									Name:      getWebhookServiceName(wh.DeploymentName),
									Path:      wh.WebhookPath,
									Port:      &wh.ContainerPort,
								},
							},
						},
					),
				),
			)
		case v1alpha1.MutatingAdmissionWebhook:
			objs = append(objs,
				GenerateMutatingWebhookConfigurationResource(
					wh.GenerateName,
					WithMutatingWebhooks(
						admissionregistrationv1.MutatingWebhook{
							Name:                    wh.GenerateName,
							Rules:                   wh.Rules,
							FailurePolicy:           wh.FailurePolicy,
							MatchPolicy:             wh.MatchPolicy,
							ObjectSelector:          wh.ObjectSelector,
							SideEffects:             wh.SideEffects,
							TimeoutSeconds:          wh.TimeoutSeconds,
							AdmissionReviewVersions: wh.AdmissionReviewVersions,
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Namespace: opts.InstallNamespace,
									Name:      getWebhookServiceName(wh.DeploymentName),
									Path:      wh.WebhookPath,
									Port:      &wh.ContainerPort,
								},
							},
							ReinvocationPolicy: wh.ReinvocationPolicy,
						},
					),
				),
			)
		case v1alpha1.ConversionWebhook:
			// dealt with using resource mutators
		}
	}

	for _, deploymentSpec := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		if _, ok := webhookServicePortsByDeployment[deploymentSpec.Name]; !ok {
			continue
		}

		servicePorts := webhookServicePortsByDeployment[deploymentSpec.Name]
		ports := servicePorts.UnsortedList()
		slices.SortFunc(ports, func(a, b corev1.ServicePort) int {
			return cmp.Compare(a.Name, b.Name)
		})

		var labelSelector map[string]string
		if deploymentSpec.Spec.Selector != nil {
			labelSelector = deploymentSpec.Spec.Selector.MatchLabels
		}

		objs = append(objs,
			GenerateServiceResource(
				getWebhookServiceName(deploymentSpec.Name),
				opts.InstallNamespace,
				WithServiceSpec(
					corev1.ServiceSpec{
						Ports:    ports,
						Selector: labelSelector,
					},
				),
			),
		)
	}

	return objs, nil
}

func BundleConversionWebhookResourceMutator(rv1 *convert.RegistryV1, opts Options) (ResourceMutators, error) {
	mutators := ResourceMutators{}

	// generate mutators based on conversion webhook definitions on the CRD
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		switch wh.Type {
		case v1alpha1.ConversionWebhook:
			conversionWebhookPath := "/"
			if wh.WebhookPath != nil {
				conversionWebhookPath = *wh.WebhookPath
			}
			conversion := &apiextensionsv1.CustomResourceConversion{
				Strategy: apiextensionsv1.WebhookConverter,
				Webhook: &apiextensionsv1.WebhookConversion{
					ClientConfig: &apiextensionsv1.WebhookClientConfig{
						Service: &apiextensionsv1.ServiceReference{
							Namespace: opts.InstallNamespace,
							Name:      getWebhookServiceName(wh.DeploymentName),
							Path:      &conversionWebhookPath,
							Port:      &wh.ContainerPort,
						},
					},
					ConversionReviewVersions: wh.AdmissionReviewVersions,
				},
			}

			for _, conversionCRD := range wh.ConversionCRDs {
				mutators.Append(
					CustomResourceDefinitionMutator(conversionCRD, func(crd *apiextensionsv1.CustomResourceDefinition) error {
						crd.Spec.Conversion = conversion
						return nil
					}),
				)
			}
		}
	}

	// generate mutators based on conversion webhook configurations already present on the CRDs
	for _, crd := range rv1.CRDs {
		if crd.Spec.Conversion != nil && crd.Spec.Conversion.Webhook != nil && crd.Spec.Conversion.Webhook.ClientConfig != nil && crd.Spec.Conversion.Webhook.ClientConfig.Service != nil {
			mutators.Append(
				CustomResourceDefinitionMutator(crd.GetName(), func(crd *apiextensionsv1.CustomResourceDefinition) error {
					crd.Spec.Conversion.Webhook.ClientConfig.Service.Namespace = opts.InstallNamespace
					return nil
				}),
			)
		}
	}

	return mutators, nil
}

func CertificateProviderResourceMutator(rv1 *convert.RegistryV1, opts Options) (ResourceMutators, error) {
	resourceMutators := ResourceMutators{}
	webhookDefnsByDeployment := map[string][]v1alpha1.WebhookDescription{}

	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		webhookDefnsByDeployment[wh.DeploymentName] = append(webhookDefnsByDeployment[wh.DeploymentName], wh)
	}

	for depName, webhooks := range webhookDefnsByDeployment {
		certCfg := getCertCfgForDeployment(depName, opts.InstallNamespace, rv1.CSV.Name)

		resourceMutators.Append(
			DeploymentResourceMutator(depName, opts.InstallNamespace, func(dep *appsv1.Deployment) error {
				dep.Spec.Template.Spec.Volumes = slices.DeleteFunc(dep.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
					return v.Name == "apiservice-cert" || v.Name == "webhook-cert"
				})
				dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes,
					corev1.Volume{
						Name: "apiservice-cert",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: certCfg.CertName,
								Items: []corev1.KeyToPath{
									{
										Key:  "tls.crt",
										Path: "apiserver.crt",
									},
									{
										Key:  "tls.key",
										Path: "apiserver.key",
									},
								},
							},
						},
					},
					corev1.Volume{
						Name: "webhook-cert",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: certCfg.CertName,
								Items: []corev1.KeyToPath{
									{
										Key:  "tls.crt",
										Path: "tls.crt",
									},
									{
										Key:  "tls.key",
										Path: "tls.key",
									},
								},
							},
						},
					},
				)

				volumeMounts := []corev1.VolumeMount{
					{Name: "apiservice-cert", MountPath: "/apiserver.local.config/certificates"},
					{Name: "webhook-cert", MountPath: "/tmp/k8s-webhook-server/serving-certs"},
				}
				for i := range dep.Spec.Template.Spec.Containers {
					dep.Spec.Template.Spec.Containers[i].VolumeMounts = slices.DeleteFunc(dep.Spec.Template.Spec.Containers[i].VolumeMounts, func(vm corev1.VolumeMount) bool {
						return vm.Name == "apiservice-cert" || vm.Name == "webhook-cert"
					})
					dep.Spec.Template.Spec.Containers[i].VolumeMounts = append(dep.Spec.Template.Spec.Containers[i].VolumeMounts, volumeMounts...)
				}

				return nil
			}),
		)

		resourceMutators.Append(
			ServiceResourceMutator(getWebhookServiceName(depName), opts.InstallNamespace, func(svc *corev1.Service) error {
				return opts.CertificateProvider.InjectCABundle(svc, certCfg)
			}),
		)

		for _, wh := range webhooks {
			switch wh.Type {
			case v1alpha1.ValidatingAdmissionWebhook:
				resourceMutators.Append(
					ValidatingWebhookConfigurationMutator(wh.GenerateName, func(whResource *admissionregistrationv1.ValidatingWebhookConfiguration) error {
						return opts.CertificateProvider.InjectCABundle(whResource, certCfg)
					}),
				)
			case v1alpha1.MutatingAdmissionWebhook:
				resourceMutators.Append(
					MutatingWebhookConfigurationMutator(wh.GenerateName, func(whResource *admissionregistrationv1.MutatingWebhookConfiguration) error {
						return opts.CertificateProvider.InjectCABundle(whResource, certCfg)
					}),
				)
			case v1alpha1.ConversionWebhook:
				for _, crdName := range wh.ConversionCRDs {
					resourceMutators.Append(
						CustomResourceDefinitionMutator(crdName, func(crd *apiextensionsv1.CustomResourceDefinition) error {
							return opts.CertificateProvider.InjectCABundle(crd, certCfg)
						}),
					)
				}
			}
		}
	}
	return resourceMutators, nil
}

func CertProviderResourceGenerator(rv1 *convert.RegistryV1, opts Options) ([]client.Object, error) {
	var objs []client.Object
	deploymentsWithWebhooks := sets.Set[string]{}

	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		deploymentsWithWebhooks.Insert(wh.DeploymentName)
	}

	for _, depName := range deploymentsWithWebhooks.UnsortedList() {
		certCfg := getCertCfgForDeployment(depName, opts.InstallNamespace, rv1.CSV.Name)
		certObjs, err := opts.CertificateProvider.AdditionalObjects(certCfg)
		if err != nil {
			return nil, err
		}
		for _, certObj := range certObjs {
			objs = append(objs, &certObj)
		}
	}
	return objs, nil
}

func getWebhookServicePort(wh v1alpha1.WebhookDescription) corev1.ServicePort {
	containerPort := int32(443)
	if wh.ContainerPort > 0 {
		containerPort = wh.ContainerPort
	}

	targetPort := intstr.FromInt32(containerPort)
	if wh.TargetPort != nil {
		targetPort = *wh.TargetPort
	}

	return corev1.ServicePort{
		Name:       strconv.Itoa(int(containerPort)),
		Port:       containerPort,
		TargetPort: targetPort,
	}
}

func getWebhookServiceName(deploymentName string) string {
	return fmt.Sprintf("%s-service", strings.ReplaceAll(deploymentName, ".", "-"))
}

func getCertCfgForDeployment(deploymentName string, deploymentNamespace string, csvName string) CertificateProvisioningConfig {
	return CertificateProvisioningConfig{
		WebhookServiceName: getWebhookServiceName(deploymentName),
		Namespace:          deploymentNamespace,
		CertName:           fmt.Sprintf("%s-%s-crt", csvName, deploymentName),
	}
}
