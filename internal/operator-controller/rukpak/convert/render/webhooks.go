package render

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
)

func WithServiceSpec(serviceSpec corev1.ServiceSpec) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *corev1.Service:
			o.Spec = serviceSpec
		}
	}
}

func WithValidatingWebhooks(webhooks ...admissionregistrationv1.ValidatingWebhook) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *admissionregistrationv1.ValidatingWebhookConfiguration:
			o.Webhooks = webhooks
		}
	}
}

func WithMutatingWebhooks(webhooks ...admissionregistrationv1.MutatingWebhook) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *admissionregistrationv1.MutatingWebhookConfiguration:
			o.Webhooks = webhooks
		}
	}
}

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

func GenerateValidatingWebhookConfigurationResource(name string, opts ...ResourceGenerationOption) *admissionregistrationv1.ValidatingWebhookConfiguration {
	return ResourceGenerationOptions(opts).ApplyTo(
		&admissionregistrationv1.ValidatingWebhookConfiguration{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ValidatingWebhookConfiguration",
				APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	).(*admissionregistrationv1.ValidatingWebhookConfiguration)
}

func GenerateMutatingWebhookConfigurationResource(name string, opts ...ResourceGenerationOption) *admissionregistrationv1.MutatingWebhookConfiguration {
	return ResourceGenerationOptions(opts).ApplyTo(
		&admissionregistrationv1.MutatingWebhookConfiguration{
			TypeMeta: metav1.TypeMeta{
				Kind:       "MutatingWebhookConfiguration",
				APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	).(*admissionregistrationv1.MutatingWebhookConfiguration)
}

func GenerateServiceResource(name string, namespace string, opts ...ResourceGenerationOption) *corev1.Service {
	return ResourceGenerationOptions(opts).ApplyTo(&corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}).(*corev1.Service)
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
