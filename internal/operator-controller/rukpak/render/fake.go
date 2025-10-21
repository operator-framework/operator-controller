package render

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type FakeCertProvider struct {
	InjectCABundleFn    func(obj client.Object, cfg CertificateProvisionerConfig) error
	AdditionalObjectsFn func(cfg CertificateProvisionerConfig) ([]unstructured.Unstructured, error)
	GetCertSecretInfoFn func(cfg CertificateProvisionerConfig) CertSecretInfo
}

func (f FakeCertProvider) InjectCABundle(obj client.Object, cfg CertificateProvisionerConfig) error {
	return f.InjectCABundleFn(obj, cfg)
}

func (f FakeCertProvider) AdditionalObjects(cfg CertificateProvisionerConfig) ([]unstructured.Unstructured, error) {
	return f.AdditionalObjectsFn(cfg)
}

func (f FakeCertProvider) GetCertSecretInfo(cfg CertificateProvisionerConfig) CertSecretInfo {
	return f.GetCertSecretInfoFn(cfg)
}
