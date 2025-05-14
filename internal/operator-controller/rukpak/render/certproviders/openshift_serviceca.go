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

const (
	openshiftServiceCAServingCertNameAnnotation = "service.beta.openshift.io/serving-cert-secret-name"
	openshiftServiceCAInjectCABundleAnnotation  = "service.beta.openshift.io/inject-cabundle"
)

var _ render.CertificateProvider = (*OpenshiftServiceCaCertificateProvider)(nil)

type OpenshiftServiceCaCertificateProvider struct{}

func (p OpenshiftServiceCaCertificateProvider) InjectCABundle(obj client.Object, cfg render.CertificateProvisionerConfig) error {
	switch obj.(type) {
	case *admissionregistrationv1.ValidatingWebhookConfiguration:
		p.addInjectCABundleAnnotation(obj)
	case *admissionregistrationv1.MutatingWebhookConfiguration:
		p.addInjectCABundleAnnotation(obj)
	case *apiextensionsv1.CustomResourceDefinition:
		p.addInjectCABundleAnnotation(obj)
	case *corev1.Service:
		p.addServingCertSecretNameAnnotation(obj, cfg.CertName)
	}
	return nil
}

func (p OpenshiftServiceCaCertificateProvider) AdditionalObjects(_ render.CertificateProvisionerConfig) ([]unstructured.Unstructured, error) {
	return nil, nil
}

func (p OpenshiftServiceCaCertificateProvider) GetCertSecretInfo(cfg render.CertificateProvisionerConfig) render.CertSecretInfo {
	return render.CertSecretInfo{
		SecretName:     cfg.CertName,
		PrivateKeyKey:  "tls.key",
		CertificateKey: "tls.crt",
	}
}

func (p OpenshiftServiceCaCertificateProvider) addServingCertSecretNameAnnotation(obj client.Object, certName string) {
	injectionAnnotation := map[string]string{
		openshiftServiceCAServingCertNameAnnotation: certName,
	}
	obj.SetAnnotations(util.MergeMaps(obj.GetAnnotations(), injectionAnnotation))
}

func (p OpenshiftServiceCaCertificateProvider) addInjectCABundleAnnotation(obj client.Object) {
	injectionAnnotation := map[string]string{
		openshiftServiceCAInjectCABundleAnnotation: "true",
	}
	obj.SetAnnotations(util.MergeMaps(obj.GetAnnotations(), injectionAnnotation))
}
