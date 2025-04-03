package render

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CertificateProvisioningConfig struct {
	WebhookServiceName string
	CertName           string
	Namespace          string
}

type CertificateProvider interface {
	InjectCABundle(obj client.Object, cfg CertificateProvisioningConfig) error
	AdditionalObjects(cfg CertificateProvisioningConfig) ([]unstructured.Unstructured, error)
	GetCertSecretInfo(cfg CertificateProvisioningConfig) CertSecretInfo
}

type CertSecretInfo struct {
	SecretName     string
	CertificateKey string
	PrivateKeyKey  string
}
