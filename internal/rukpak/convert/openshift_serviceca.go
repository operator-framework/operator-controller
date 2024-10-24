package convert

import (
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func WithOpenShiftServiceCACertificateProvider() ToHelmChartOption {
	return func(options *toChartOptions) {
		options.certProvider = openshiftServiceCaCertificateProvider{}
	}
}

type openshiftServiceCaCertificateProvider struct{}

const (
	openshiftServiceCaCertificateProviderName = "openshift-service-ca"
)

func init() {
	certProviders[openshiftServiceCaCertificateProviderName] = openshiftServiceCaCertificateProvider{}
}

func (openshiftServiceCaCertificateProvider) ModifyValidatingWebhookConfiguration(wh *admissionregistrationv1.ValidatingWebhookConfiguration, _ Namer) error {
	if wh.Annotations == nil {
		wh.Annotations = map[string]string{}
	}
	wh.Annotations["service.beta.openshift.io/inject-cabundle"] = "true"
	return nil
}

func (openshiftServiceCaCertificateProvider) ModifyMutatingWebhookConfiguration(wh *admissionregistrationv1.MutatingWebhookConfiguration, _ Namer) error {
	if wh.Annotations == nil {
		wh.Annotations = map[string]string{}
	}
	wh.Annotations["service.beta.openshift.io/inject-cabundle"] = "true"
	return nil
}

func (openshiftServiceCaCertificateProvider) ModifyCustomResourceDefinition(crd *apiextensionsv1.CustomResourceDefinition, _ Namer) error {
	if crd.Annotations == nil {
		crd.Annotations = map[string]string{}
	}
	crd.Annotations["service.beta.openshift.io/inject-cabundle"] = "true"
	return nil
}

func (p openshiftServiceCaCertificateProvider) ModifyService(svc *corev1.Service, namer Namer) error {
	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}
	svc.Annotations["service.beta.openshift.io/serving-cert-secret-name"] = p.secretName(namer)
	return nil
}

func (openshiftServiceCaCertificateProvider) AdditionalObjects(_ Namer) ([]unstructured.Unstructured, error) {
	return nil, nil
}

func (p openshiftServiceCaCertificateProvider) CertSecretInfo(namer Namer) CertSecretInfo {
	return CertSecretInfo{
		SecretName:     p.secretName(namer),
		CertificateKey: "tls.crt",
		PrivateKeyKey:  "tls.key",
	}
}

func (openshiftServiceCaCertificateProvider) secretName(namer Namer) string {
	return fmt.Sprintf("%s-%s-cert", namer.CSVName(), namer.DeploymentName())
}

var _ CertificateProvider = (*openshiftServiceCaCertificateProvider)(nil)
