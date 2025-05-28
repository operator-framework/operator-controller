package certproviders

import (
	"errors"
	"fmt"
	"time"

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
	olmv0RotationPeriod           = 730 * 24 * time.Hour // 2 year rotation
	olmv0RenewBefore              = 24 * time.Hour       // renew certificate within 24h of expiry
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

	// OLMv0 parity:
	// - self-signed issuer
	// - 2 year rotation period
	// - renew 24h before expiry
	// - CN: argocd-operator-controller-manager-service.argocd (<deploymentName>-service.<namespace>)
	// - CA: false
	// - DNS:argocd-operator-controller-manager-service.argocd, DNS:argocd-operator-controller-manager-service.argocd.svc, DNS:argocd-operator-controller-manager-service.argocd.svc.cluster.local

	// Full example of OLMv0 Certificate data (argocd-operator.v0.8.0):
	//Certificate:
	//    Data:
	//        Version: 3 (0x2)
	//        Serial Number: 1507821748758744637 (0x14ecdbe4475f8e3d)
	//        Signature Algorithm: ecdsa-with-SHA256
	//        Issuer: O=Red Hat, Inc., CN=olm-selfsigned-275dd2a363db7513
	//        Validity
	//            Not Before: May 12 11:15:02 2025 GMT
	//            Not After : May 12 11:15:02 2027 GMT
	//        Subject: O=Red Hat, Inc., CN=argocd-operator-controller-manager-service.argocd
	//        Subject Public Key Info:
	//            Public Key Algorithm: id-ecPublicKey
	//                Public-Key: (256 bit)
	//                pub: ...
	//                ASN1 OID: prime256v1
	//                NIST CURVE: P-256
	//        X509v3 extensions:
	//            X509v3 Extended Key Usage:
	//                TLS Web Server Authentication
	//            X509v3 Basic Constraints: critical
	//                CA:FALSE
	//            X509v3 Authority Key Identifier: ...
	//            X509v3 Subject Alternative Name:
	//                DNS:argocd-operator-controller-manager-service.argocd, DNS:argocd-operator-controller-manager-service.argocd.svc, DNS:argocd-operator-controller-manager-service.argocd.svc.cluster.local
	//    Signature Algorithm: ecdsa-with-SHA256
	//    Signature Value: ...

	// Full example of OLMv1 certificate for argocd-operator v0.8.0 with the Issuer and Certificate settings that follow:
	//Certificate:
	//    Data:
	//        Version: 3 (0x2)
	//        Serial Number:
	//            d5:8f:4f:ae:b1:67:59:9d:fe:53:b5:41:d3:10:5a:2b
	//        Signature Algorithm: sha256WithRSAEncryption
	//        Issuer: CN=argocd-operator-controller-manager-service.argocd
	//        Validity
	//            Not Before: May 12 11:55:28 2025 GMT
	//            Not After : May 12 11:55:28 2027 GMT
	//        Subject: CN=argocd-operator-controller-manager-service.argocd
	//        Subject Public Key Info:
	//            Public Key Algorithm: rsaEncryption
	//                Public-Key: (2048 bit)
	//                Modulus: ...
	//                Exponent: 65537 (0x10001)
	//        X509v3 extensions:
	//            X509v3 Extended Key Usage:
	//                TLS Web Server Authentication
	//            X509v3 Basic Constraints: critical
	//                CA:FALSE
	//            X509v3 Subject Alternative Name:
	//                DNS:argocd-operator-controller-manager-service.argocd, DNS:argocd-operator-controller-manager-service.argocd.svc, DNS:argocd-operator-controller-manager-service.argocd.svc.cluster.local
	//    Signature Algorithm: sha256WithRSAEncryption
	//    Signature Value: ...

	// Notes:
	// - the Organization "Red Hat, Inc." will not be used to avoid any hard links between Red Hat and the operator-controller project
	// - for OLMv1 we'll use the default algorithm settings and key size (2048) coming from cert-manager as this is deemed more secure

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
			CommonName: fmt.Sprintf("%s.%s", cfg.WebhookServiceName, cfg.Namespace),
			Usages:     []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth},
			IsCA:       false,
			DNSNames: []string{
				fmt.Sprintf("%s.%s", cfg.WebhookServiceName, cfg.Namespace),
				fmt.Sprintf("%s.%s.svc", cfg.WebhookServiceName, cfg.Namespace),
				fmt.Sprintf("%s.%s.svc.cluster.local", cfg.WebhookServiceName, cfg.Namespace),
			},
			IssuerRef: certmanagermetav1.ObjectReference{
				Name: issuer.GetName(),
			},
			Duration: &metav1.Duration{
				Duration: olmv0RotationPeriod,
			},
			RenewBefore: &metav1.Duration{
				Duration: olmv0RenewBefore,
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
