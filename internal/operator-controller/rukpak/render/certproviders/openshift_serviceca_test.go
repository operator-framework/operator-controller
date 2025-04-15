package certproviders_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/certproviders"
)

func Test_OpenshiftServiceCACertificateProvider_InjectCABundle(t *testing.T) {
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
						"service.beta.openshift.io/inject-cabundle": "true",
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
						"service.beta.openshift.io/inject-cabundle": "true",
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
						"service.beta.openshift.io/inject-cabundle": "true",
					},
				},
			},
		},
		{
			name: "injects certificate annotation in service resource",
			obj:  &corev1.Service{},
			cfg: render.CertificateProvisionerConfig{
				WebhookServiceName: "webhook-service",
				Namespace:          "namespace",
				CertName:           "cert-name",
			},
			expectedObj: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.openshift.io/serving-cert-secret-name": "cert-name",
					},
				},
			},
		},
		{
			name: "ignores other objects",
			obj:  &corev1.Secret{},
			cfg: render.CertificateProvisionerConfig{
				WebhookServiceName: "webhook-service",
				Namespace:          "namespace",
				CertName:           "cert-name",
			},
			expectedObj: &corev1.Secret{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			certProvier := certproviders.OpenshiftServiceCACertificateProvider{}
			require.NoError(t, certProvier.InjectCABundle(tc.obj, tc.cfg))
			require.Equal(t, tc.expectedObj, tc.obj)
		})
	}
}

func Test_OpenshiftServiceCACertificateProvider_AdditionalObjects(t *testing.T) {
	certProvier := certproviders.OpenshiftServiceCACertificateProvider{}
	objs, err := certProvier.AdditionalObjects(render.CertificateProvisionerConfig{
		WebhookServiceName: "webhook-service",
		Namespace:          "namespace",
		CertName:           "cert-name",
	})
	require.NoError(t, err)
	require.Nil(t, objs)
}

func Test_OpenshiftServiceCACertificateProvider_GetCertSecretInfo(t *testing.T) {
	certProvier := certproviders.OpenshiftServiceCACertificateProvider{}
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
