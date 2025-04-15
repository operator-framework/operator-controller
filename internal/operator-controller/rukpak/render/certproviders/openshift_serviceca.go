package certproviders

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

var _ render.CertificateProvider = (*OpenshiftServiceCACertificateProvider)(nil)

type OpenshiftServiceCACertificateProvider struct{}

func (o OpenshiftServiceCACertificateProvider) InjectCABundle(obj client.Object, cfg render.CertificateProvisionerConfig) error {
	switch obj.(type) {
	case *admissionregistrationv1.ValidatingWebhookConfiguration:
		o.addCAInjectionAnnotation(obj)
	case *admissionregistrationv1.MutatingWebhookConfiguration:
		o.addCAInjectionAnnotation(obj)
	case *apiextensionsv1.CustomResourceDefinition:
		o.addCAInjectionAnnotation(obj)
	case *corev1.Service:
		o.addServingSecretNameAnnotation(obj, cfg.CertName)
	}
	return nil
}

func (o OpenshiftServiceCACertificateProvider) AdditionalObjects(_ render.CertificateProvisionerConfig) ([]unstructured.Unstructured, error) {
	return nil, nil
}

func (o OpenshiftServiceCACertificateProvider) GetCertSecretInfo(cfg render.CertificateProvisionerConfig) render.CertSecretInfo {
	return render.CertSecretInfo{
		SecretName:     cfg.CertName,
		CertificateKey: "tls.crt",
		PrivateKeyKey:  "tls.key",
	}
}

func (o OpenshiftServiceCACertificateProvider) addCAInjectionAnnotation(obj client.Object) {
	injectionAnnotation := map[string]string{
		"service.beta.openshift.io/inject-cabundle": "true",
	}
	obj.SetAnnotations(util.MergeMaps(obj.GetAnnotations(), injectionAnnotation))
}

func (o OpenshiftServiceCACertificateProvider) addServingSecretNameAnnotation(obj client.Object, certName string) {
	injectionAnnotation := map[string]string{
		"service.beta.openshift.io/serving-cert-secret-name": certName,
	}
	obj.SetAnnotations(util.MergeMaps(obj.GetAnnotations(), injectionAnnotation))
}
