package certproviders

import (
	"errors"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

const (
	certManagerInjectCAAnnotation = "cert-manager.io/inject-ca-from"
)

var _ render.CertificateProvider = (*CertManagerCertificateProvider)(nil)

type CertManagerCertificateProvider struct{}

func (p CertManagerCertificateProvider) InjectCABundle(obj client.Object, cfg render.CertificateProvisionerConfig) error {
	switch obj.(type) {
	case *admissionregistrationv1.ValidatingWebhookConfiguration:
		p.addCAInjectionAnnotation(obj, cfg.Namespace, cfg.CertName)
	case *admissionregistrationv1.MutatingWebhookConfiguration:
		p.addCAInjectionAnnotation(obj, cfg.Namespace, cfg.CertName)
	case *apiextensionsv1.CustomResourceDefinition:
		p.addCAInjectionAnnotation(obj, cfg.Namespace, cfg.CertName)
	}
	return nil
}

func (p CertManagerCertificateProvider) GetCertSecretInfo(cfg render.CertificateProvisionerConfig) render.CertSecretInfo {
	return render.CertSecretInfo{
		SecretName:     cfg.CertName,
		PrivateKeyKey:  "tls.key",
		CertificateKey: "tls.crt",
	}
}

func (p CertManagerCertificateProvider) AdditionalObjects(cfg render.CertificateProvisionerConfig) ([]unstructured.Unstructured, error) {
	var (
		objs []unstructured.Unstructured
		errs []error
	)

	issuer := &certmanagerv1.Issuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: certmanagerv1.SchemeGroupVersion.String(),
			Kind:       "Issuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.ObjectNameForBaseAndSuffix(cfg.CertName, "selfsigned-issuer"),
			Namespace: cfg.Namespace,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: &certmanagerv1.SelfSignedIssuer{},
			},
		},
	}
	issuerObj, err := util.ToUnstructured(issuer)
	if err != nil {
		errs = append(errs, err)
	} else {
		objs = append(objs, *issuerObj)
	}

	certificate := &certmanagerv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: certmanagerv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.CertName,
			Namespace: cfg.Namespace,
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: cfg.CertName,
			Usages:     []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth},
			DNSNames:   []string{fmt.Sprintf("%s.%s.svc", cfg.WebhookServiceName, cfg.Namespace)},
			IssuerRef: certmanagermetav1.ObjectReference{
				Name: issuer.GetName(),
			},
		},
	}
	certObj, err := util.ToUnstructured(certificate)
	if err != nil {
		errs = append(errs, err)
	} else {
		objs = append(objs, *certObj)
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return objs, nil
}

func (p CertManagerCertificateProvider) addCAInjectionAnnotation(obj client.Object, certNamespace string, certName string) {
	injectionAnnotation := map[string]string{
		certManagerInjectCAAnnotation: fmt.Sprintf("%s/%s", certNamespace, certName),
	}
	obj.SetAnnotations(util.MergeMaps(obj.GetAnnotations(), injectionAnnotation))
}
