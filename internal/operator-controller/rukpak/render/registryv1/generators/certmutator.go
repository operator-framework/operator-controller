package generators

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
)

// CertMutator wires all certificate-dependent concerns across the object set produced by the
// other mutators. It runs last so it can read every base object and decorate it in place:
//   - injects certificate volumes/mounts into webhook-serving Deployments
//   - sets the ClientConfig service reference and CA bundle on admission webhook configurations
//   - sets the conversion webhook configuration and CA bundle on CRDs referenced by conversion webhooks
//   - creates the webhook-serving Service object(s) (with CA bundle injected)
//   - emits any additional provider objects (e.g. Issuer/Certificate)
//
// Certificate provider-dependent steps (CA bundle injection, cert volumes, additional objects)
// are no-ops when no CertificateProvider is configured.
func CertMutator(ctx *render.Context) error {
	if ctx.RV1 == nil {
		return fmt.Errorf("bundle cannot be nil")
	}

	// index the base objects the other mutators produced so we can decorate them in place
	deploymentsByName := map[string]*appsv1.Deployment{}
	validatingByName := map[string]*admissionregistrationv1.ValidatingWebhookConfiguration{}
	mutatingByName := map[string]*admissionregistrationv1.MutatingWebhookConfiguration{}
	crdsByName := map[string]*apiextensionsv1.CustomResourceDefinition{}
	for _, obj := range ctx.Objects {
		switch o := obj.(type) {
		case *appsv1.Deployment:
			deploymentsByName[o.Name] = o
		case *admissionregistrationv1.ValidatingWebhookConfiguration:
			validatingByName[o.Name] = o
		case *admissionregistrationv1.MutatingWebhookConfiguration:
			mutatingByName[o.Name] = o
		case *apiextensionsv1.CustomResourceDefinition:
			crdsByName[o.Name] = o
		}
	}

	if err := injectDeploymentCertVolumes(ctx, deploymentsByName); err != nil {
		return err
	}
	if err := wireWebhookConfigurations(ctx, validatingByName, mutatingByName); err != nil {
		return err
	}
	if err := wireConversionWebhookCRDs(ctx, crdsByName); err != nil {
		return err
	}
	if err := appendWebhookServices(ctx); err != nil {
		return err
	}
	return appendCertProviderObjects(ctx)
}

// injectDeploymentCertVolumes adds certificate volumes/mounts to each webhook-serving deployment
// when a certificate provider is configured.
func injectDeploymentCertVolumes(ctx *render.Context, deploymentsByName map[string]*appsv1.Deployment) error {
	webhookDeployments := sets.Set[string]{}
	for _, wh := range ctx.RV1.CSV.Spec.WebhookDefinitions {
		webhookDeployments.Insert(wh.DeploymentName)
	}

	for depName := range webhookDeployments {
		dep, ok := deploymentsByName[depName]
		if !ok {
			continue
		}
		secretInfo := render.CertProvisionerFor(depName, ctx).GetCertSecretInfo()
		if secretInfo != nil {
			ensureCorrectDeploymentCertVolumes(dep, *secretInfo)
		}
	}
	return nil
}

// wireWebhookConfigurations sets the ClientConfig service reference and injects the CA bundle on
// each admission webhook configuration produced by the base webhook mutators.
func wireWebhookConfigurations(
	ctx *render.Context,
	validatingByName map[string]*admissionregistrationv1.ValidatingWebhookConfiguration,
	mutatingByName map[string]*admissionregistrationv1.MutatingWebhookConfiguration,
) error {
	for _, wh := range ctx.RV1.CSV.Spec.WebhookDefinitions {
		webhookName := strings.TrimSuffix(wh.GenerateName, "-")
		certProvisioner := render.CertProvisionerFor(wh.DeploymentName, ctx)
		containerPort := wh.ContainerPort

		switch wh.Type {
		case v1alpha1.ValidatingAdmissionWebhook:
			cfg, ok := validatingByName[webhookName]
			if !ok {
				continue
			}
			for i := range cfg.Webhooks {
				cfg.Webhooks[i].ClientConfig = admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Namespace: ctx.InstallNamespace,
						Name:      certProvisioner.ServiceName,
						Path:      wh.WebhookPath,
						Port:      &containerPort,
					},
				}
			}
			if err := certProvisioner.InjectCABundle(cfg); err != nil {
				return err
			}
		case v1alpha1.MutatingAdmissionWebhook:
			cfg, ok := mutatingByName[webhookName]
			if !ok {
				continue
			}
			for i := range cfg.Webhooks {
				cfg.Webhooks[i].ClientConfig = admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Namespace: ctx.InstallNamespace,
						Name:      certProvisioner.ServiceName,
						Path:      wh.WebhookPath,
						Port:      &containerPort,
					},
				}
			}
			if err := certProvisioner.InjectCABundle(cfg); err != nil {
				return err
			}
		}
	}
	return nil
}

// wireConversionWebhookCRDs sets spec.conversion and injects the CA bundle on each CRD referenced
// by a conversion webhook in the bundle's CSV.
func wireConversionWebhookCRDs(ctx *render.Context, crdsByName map[string]*apiextensionsv1.CustomResourceDefinition) error {
	// collect the conversion webhook for each referenced CRD
	crdToWebhook := map[string]v1alpha1.WebhookDescription{}
	for _, wh := range ctx.RV1.CSV.Spec.WebhookDefinitions {
		if wh.Type != v1alpha1.ConversionWebhook {
			continue
		}
		for _, crdName := range wh.ConversionCRDs {
			if _, ok := crdToWebhook[crdName]; ok {
				return fmt.Errorf("custom resource definition '%s' is referenced by multiple conversion webhook definitions", crdName)
			}
			crdToWebhook[crdName] = wh
		}
	}

	for crdName, cw := range crdToWebhook {
		crd, ok := crdsByName[crdName]
		if !ok {
			continue
		}

		// OLMv0 behaviour parity
		// See https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/install/webhook.go#L232
		if crd.Spec.PreserveUnknownFields {
			return fmt.Errorf("custom resource definition '%s' must have .spec.preserveUnknownFields set to false to let API Server call webhook to do the conversion", crdName)
		}

		// OLMv0 behaviour parity
		// https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/install/webhook.go#L242
		conversionWebhookPath := "/"
		if cw.WebhookPath != nil {
			conversionWebhookPath = *cw.WebhookPath
		}

		certProvisioner := render.CertProvisionerFor(cw.DeploymentName, ctx)
		containerPort := cw.ContainerPort
		crd.Spec.Conversion = &apiextensionsv1.CustomResourceConversion{
			Strategy: apiextensionsv1.WebhookConverter,
			Webhook: &apiextensionsv1.WebhookConversion{
				ClientConfig: &apiextensionsv1.WebhookClientConfig{
					Service: &apiextensionsv1.ServiceReference{
						Namespace: ctx.InstallNamespace,
						Name:      certProvisioner.ServiceName,
						Path:      &conversionWebhookPath,
						Port:      &containerPort,
					},
				},
				ConversionReviewVersions: cw.AdmissionReviewVersions,
			},
		}

		if err := certProvisioner.InjectCABundle(crd); err != nil {
			return err
		}
	}
	return nil
}

// appendWebhookServices creates the Service object(s) backing the bundle's webhooks and appends
// them to the context, injecting the CA bundle where a provider is configured.
func appendWebhookServices(ctx *render.Context) error {
	rv1 := ctx.RV1

	// collect webhook service ports
	webhookServicePortsByDeployment := map[string]sets.Set[corev1.ServicePort]{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if _, ok := webhookServicePortsByDeployment[wh.DeploymentName]; !ok {
			webhookServicePortsByDeployment[wh.DeploymentName] = sets.Set[corev1.ServicePort]{}
		}
		webhookServicePortsByDeployment[wh.DeploymentName].Insert(getWebhookServicePort(wh))
	}

	objs := make([]client.Object, 0, len(webhookServicePortsByDeployment))
	for _, deploymentSpec := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		if _, ok := webhookServicePortsByDeployment[deploymentSpec.Name]; !ok {
			continue
		}

		servicePorts := webhookServicePortsByDeployment[deploymentSpec.Name]
		ports := servicePorts.UnsortedList()
		slices.SortStableFunc(ports, func(a, b corev1.ServicePort) int {
			return cmp.Or(cmp.Compare(a.Port, b.Port), cmp.Compare(a.TargetPort.IntValue(), b.TargetPort.IntValue()))
		})

		var labelSelector map[string]string
		if deploymentSpec.Spec.Selector != nil {
			labelSelector = deploymentSpec.Spec.Selector.MatchLabels
		}

		certProvisioner := render.CertProvisionerFor(deploymentSpec.Name, ctx)
		serviceResource := CreateServiceResource(
			certProvisioner.ServiceName,
			ctx.InstallNamespace,
			WithServiceSpec(
				corev1.ServiceSpec{
					Ports:    ports,
					Selector: labelSelector,
				},
			),
		)

		if err := certProvisioner.InjectCABundle(serviceResource); err != nil {
			return err
		}
		objs = append(objs, serviceResource)
	}

	ctx.Objects = append(ctx.Objects, objs...)
	return nil
}

// appendCertProviderObjects appends any additional objects the certificate provider needs to
// function (e.g. Issuer or Certificate resources).
func appendCertProviderObjects(ctx *render.Context) error {
	deploymentsWithWebhooks := sets.Set[string]{}
	for _, wh := range ctx.RV1.CSV.Spec.WebhookDefinitions {
		deploymentsWithWebhooks.Insert(wh.DeploymentName)
	}

	var objs []client.Object
	for _, depName := range deploymentsWithWebhooks.UnsortedList() {
		certCfg := render.CertProvisionerFor(depName, ctx)
		certObjs, err := certCfg.AdditionalObjects()
		if err != nil {
			return err
		}
		for _, certObj := range certObjs {
			objs = append(objs, &certObj)
		}
	}
	ctx.Objects = append(ctx.Objects, objs...)
	return nil
}
