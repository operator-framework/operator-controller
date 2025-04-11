package render

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

// CertificateProvider encapsulate the creation and modification of object for certificate provisioning
// in Kubernetes by vendors such as CertManager or the OpenshiftServiceCA operator
type CertificateProvider interface {
	InjectCABundle(obj client.Object, cfg CertificateProvisionerConfig) error
	AdditionalObjects(cfg CertificateProvisionerConfig) ([]unstructured.Unstructured, error)
	GetCertSecretInfo(cfg CertificateProvisionerConfig) CertSecretInfo
}

// CertSecretInfo contains describes the certificate secret resource information such as name and
// certificate and private key keys
type CertSecretInfo struct {
	SecretName     string
	CertificateKey string
	PrivateKeyKey  string
}

// CertificateProvisionerConfig contains the necessary information for a CertificateProvider
// to correctly generate and modify object for certificate injection and automation
type CertificateProvisionerConfig struct {
	WebhookServiceName string
	CertName           string
	Namespace          string
	CertProvider       CertificateProvider
}

// CertificateProvisioner uses a CertificateProvider to modify and generate objects based on its
// CertificateProvisionerConfig
type CertificateProvisioner CertificateProvisionerConfig

func (c CertificateProvisioner) InjectCABundle(obj client.Object) error {
	if c.CertProvider == nil {
		return nil
	}
	return c.CertProvider.InjectCABundle(obj, CertificateProvisionerConfig(c))
}

func (c CertificateProvisioner) AdditionalObjects() ([]unstructured.Unstructured, error) {
	if c.CertProvider == nil {
		return nil, nil
	}
	return c.CertProvider.AdditionalObjects(CertificateProvisionerConfig(c))
}

func (c CertificateProvisioner) GetCertSecretInfo() *CertSecretInfo {
	if c.CertProvider == nil {
		return nil
	}
	info := c.CertProvider.GetCertSecretInfo(CertificateProvisionerConfig(c))
	return &info
}

func CertProvisionerFor(deploymentName string, opts Options) CertificateProvisioner {
	// maintaining parity with OLMv0 naming
	// See https://github.com/operator-framework/operator-lifecycle-manager/blob/658a6a60de8315f055f54aa7e50771ee4daa8983/pkg/controller/install/webhook.go#L254
	webhookServiceName := util.ObjectNameForBaseAndSuffix(strings.ReplaceAll(deploymentName, ".", "-"), "service")

	// maintaining parity with cert secret name in OLMv0
	// See https://github.com/operator-framework/operator-lifecycle-manager/blob/658a6a60de8315f055f54aa7e50771ee4daa8983/pkg/controller/install/certresources.go#L151
	certName := util.ObjectNameForBaseAndSuffix(webhookServiceName, "cert")

	return CertificateProvisioner{
		CertProvider:       opts.CertificateProvider,
		WebhookServiceName: webhookServiceName,
		Namespace:          opts.InstallNamespace,
		CertName:           certName,
	}
}
