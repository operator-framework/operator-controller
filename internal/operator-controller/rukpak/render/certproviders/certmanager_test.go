package certproviders_test

import (
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/certproviders"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

func Test_CertManagerProvider_InjectCABundle(t *testing.T) {
	for _, tc := range []struct {
		name        string
		obj         client.Object
		cfg         render.CertificateProvisionerConfig
		expectedObj client.Object
	}{
		{
			name: "injects certificate annotation in validating webhook configuration",
			obj:  &admissionregistrationv1.ValidatingWebhookConfiguration{},
			cfg: render.CertificateProvisionerConfig{
				WebhookServiceName: "webhook-service",
				Namespace:          "namespace",
				CertName:           "cert-name",
			},
			expectedObj: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"cert-manager.io/inject-ca-from": "namespace/cert-name",
					},
				},
			},
		},
		{
			name: "injects certificate annotation in mutating webhook configuration",
			obj:  &admissionregistrationv1.MutatingWebhookConfiguration{},
			cfg: render.CertificateProvisionerConfig{
				WebhookServiceName: "webhook-service",
				Namespace:          "namespace",
				CertName:           "cert-name",
			},
			expectedObj: &admissionregistrationv1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"cert-manager.io/inject-ca-from": "namespace/cert-name",
					},
				},
			},
		},
		{
			name: "injects certificate annotation in custom resource definition",
			obj:  &apiextensionsv1.CustomResourceDefinition{},
			cfg: render.CertificateProvisionerConfig{
				WebhookServiceName: "webhook-service",
				Namespace:          "namespace",
				CertName:           "cert-name",
			},
			expectedObj: &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"cert-manager.io/inject-ca-from": "namespace/cert-name",
					},
				},
			},
		},
		{
			name: "ignores other objects",
			obj:  &corev1.Service{},
			cfg: render.CertificateProvisionerConfig{
				WebhookServiceName: "webhook-service",
				Namespace:          "namespace",
				CertName:           "cert-name",
			},
			expectedObj: &corev1.Service{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			certProvier := certproviders.CertManagerCertificateProvider{}
			require.NoError(t, certProvier.InjectCABundle(tc.obj, tc.cfg))
			require.Equal(t, tc.expectedObj, tc.obj)
		})
	}
}

func Test_CertManagerProvider_AdditionalObjects(t *testing.T) {
	certProvier := certproviders.CertManagerCertificateProvider{}
	objs, err := certProvier.AdditionalObjects(render.CertificateProvisionerConfig{
		WebhookServiceName: "webhook-service",
		Namespace:          "namespace",
		CertName:           "cert-name",
	})
	require.NoError(t, err)
	require.Equal(t, []unstructured.Unstructured{
		toUnstructured(t, &certmanagerv1.Issuer{
			TypeMeta: metav1.TypeMeta{
				APIVersion: certmanagerv1.SchemeGroupVersion.String(),
				Kind:       "Issuer",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cert-name-selfsigned-issuer",
				Namespace: "namespace",
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					SelfSigned: &certmanagerv1.SelfSignedIssuer{},
				},
			},
		}),
		toUnstructured(t, &certmanagerv1.Certificate{
			TypeMeta: metav1.TypeMeta{
				APIVersion: certmanagerv1.SchemeGroupVersion.String(),
				Kind:       "Certificate",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cert-name",
				Namespace: "namespace",
			},
			Spec: certmanagerv1.CertificateSpec{
				SecretName: "cert-name",
				Usages:     []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth},
				CommonName: "webhook-service.namespace",
				IsCA:       false,
				DNSNames: []string{
					"webhook-service.namespace",
					"webhook-service.namespace.svc",
					"webhook-service.namespace.svc.cluster.local",
				},
				IssuerRef: certmanagermetav1.ObjectReference{
					Name: "cert-name-selfsigned-issuer",
				},
				Duration: &metav1.Duration{
					// OLMv0 has a 2 year certificate rotation period
					Duration: 730 * 24 * time.Hour,
				},
				RenewBefore: &metav1.Duration{
					// OLMv0 reviews 24h before expiry
					Duration: 24 * time.Hour,
				},
			},
		}),
	}, objs)
}

func Test_CertManagerProvider_GetCertSecretInfo(t *testing.T) {
	certProvier := certproviders.CertManagerCertificateProvider{}
	certInfo := certProvier.GetCertSecretInfo(render.CertificateProvisionerConfig{
		WebhookServiceName: "webhook-service",
		Namespace:          "namespace",
		CertName:           "cert-name",
	})
	require.Equal(t, render.CertSecretInfo{
		SecretName:     "cert-name",
		PrivateKeyKey:  "tls.key",
		CertificateKey: "tls.crt",
	}, certInfo)
}

func toUnstructured(t *testing.T, obj client.Object) unstructured.Unstructured {
	u, err := util.ToUnstructured(obj)
	require.NoError(t, err)
	return *u
}
