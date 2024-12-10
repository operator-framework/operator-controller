package convert

import (
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Namer interface {
	CSVName() string
	DeploymentName() string
	ServiceName() string
}

type CertificateProvider interface {
	ModifyService(*corev1.Service, Namer) error
	ModifyValidatingWebhookConfiguration(*admissionregistrationv1.ValidatingWebhookConfiguration, Namer) error
	ModifyMutatingWebhookConfiguration(*admissionregistrationv1.MutatingWebhookConfiguration, Namer) error
	ModifyCustomResourceDefinition(*apiextensionsv1.CustomResourceDefinition, Namer) error
	AdditionalObjects(Namer) ([]unstructured.Unstructured, error)
	CertSecretInfo(Namer) CertSecretInfo
}

type CertSecretInfo struct {
	SecretName     string
	CertificateKey string
	PrivateKeyKey  string
}

var certProviders = map[string]CertificateProvider{}

func CertProviderByName(certProviderName string) (CertificateProvider, error) {
	if certProviderName == "" {
		return nil, nil
	}
	cp, ok := certProviders[certProviderName]
	if !ok {
		return nil, fmt.Errorf("unknown certificate provider %q", certProviderName)
	}
	return cp, nil
}

func WithCertificateProvider(certProvider CertificateProvider) ToHelmChartOption {
	return func(options *toChartOptions) {
		options.certProvider = certProvider
	}
}
