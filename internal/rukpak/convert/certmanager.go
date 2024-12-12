package convert

import (
	"errors"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func WithCertManagerCertificateProvider() ToHelmChartOption {
	return func(options *toChartOptions) {
		options.certProvider = certManagerCertificateProvider{}
	}
}

type certManagerCertificateProvider struct{}

const (
	certManagerCertificateProviderName = "cert-manager"
)

func init() {
	certProviders[certManagerCertificateProviderName] = certManagerCertificateProvider{}
}

func (p certManagerCertificateProvider) ModifyValidatingWebhookConfiguration(wh *admissionregistrationv1.ValidatingWebhookConfiguration, namer Namer) error {
	if wh.Annotations == nil {
		wh.Annotations = map[string]string{}
	}
	wh.Annotations["cert-manager.io/inject-ca-from"] = fmt.Sprintf("{{ .Release.Namespace }}/%s", p.certName(namer))
	return nil
}

func (p certManagerCertificateProvider) ModifyMutatingWebhookConfiguration(wh *admissionregistrationv1.MutatingWebhookConfiguration, namer Namer) error {
	if wh.Annotations == nil {
		wh.Annotations = map[string]string{}
	}
	wh.Annotations["cert-manager.io/inject-ca-from"] = fmt.Sprintf("{{ .Release.Namespace }}/%s", p.certName(namer))
	return nil
}

func (p certManagerCertificateProvider) ModifyCustomResourceDefinition(crd *apiextensionsv1.CustomResourceDefinition, namer Namer) error {
	if crd.Annotations == nil {
		crd.Annotations = map[string]string{}
	}
	crd.Annotations["cert-manager.io/inject-ca-from"] = fmt.Sprintf("{{ .Release.Namespace }}/%s", p.certName(namer))
	return nil
}

func (certManagerCertificateProvider) ModifyService(_ *corev1.Service, _ Namer) error {
	return nil
}

func (p certManagerCertificateProvider) CertSecretInfo(namer Namer) CertSecretInfo {
	return CertSecretInfo{
		SecretName:     p.secretName(namer),
		CertificateKey: "tls.crt",
		PrivateKeyKey:  "tls.key",
	}
}

func (p certManagerCertificateProvider) AdditionalObjects(namer Namer) ([]unstructured.Unstructured, error) {
	var (
		objs []unstructured.Unstructured
		errs []error
	)

	issuerName := fmt.Sprintf("%s-%s-selfsigned-issuer", namer.CSVName(), namer.DeploymentName())
	issuer := &certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name: issuerName,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: &certmanagerv1.SelfSignedIssuer{},
			},
		},
	}
	issuer.SetGroupVersionKind(certmanagerv1.SchemeGroupVersion.WithKind("Issuer"))
	issuerObj, err := toUnstructured(issuer)
	if err != nil {
		errs = append(errs, err)
	} else {
		objs = append(objs, *issuerObj)
	}

	certificate := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name: p.certName(namer),
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: p.secretName(namer),
			Usages:     []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth},
			DNSNames:   []string{fmt.Sprintf("%s.{{ .Release.Namespace }}.svc", namer.ServiceName())},
			IssuerRef: certmanagermetav1.ObjectReference{
				Name: issuerName,
			},
		},
	}
	certificate.SetGroupVersionKind(certmanagerv1.SchemeGroupVersion.WithKind("Certificate"))
	certObj, err := toUnstructured(certificate)
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

func (p certManagerCertificateProvider) certName(namer Namer) string {
	return namer.DeploymentName()
}

func (p certManagerCertificateProvider) secretName(namer Namer) string {
	return fmt.Sprintf("%s-%s-cert", namer.CSVName(), namer.DeploymentName())
}
