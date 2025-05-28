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

func Test_OpenshiftServiceCAProvider_InjectCABundle(t *testing.T) {
	for _, tc := range []struct {
		name        string
		obj         client.Object
		cfg         render.CertificateProvisionerConfig
		expectedObj client.Object
	}{
		{
			name: "injects inject-cabundle annotation in validating webhook configuration",
			obj:  &admissionregistrationv1.ValidatingWebhookConfiguration{},
			cfg: render.CertificateProvisionerConfig{
				ServiceName: "webhook-service",
				Namespace:   "namespace",
				CertName:    "cert-name",
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
			name: "injects inject-cabundle annotation in mutating webhook configuration",
			obj:  &admissionregistrationv1.MutatingWebhookConfiguration{},
			cfg: render.CertificateProvisionerConfig{
				ServiceName: "webhook-service",
				Namespace:   "namespace",
				CertName:    "cert-name",
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
			name: "injects inject-cabundle annotation in custom resource definition",
			obj:  &apiextensionsv1.CustomResourceDefinition{},
			cfg: render.CertificateProvisionerConfig{
				ServiceName: "webhook-service",
				Namespace:   "namespace",
				CertName:    "cert-name",
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
			name: "injects serving-cert-secret-name annotation in service resource referencing the certificate name",
			obj:  &corev1.Service{},
			cfg: render.CertificateProvisionerConfig{
				ServiceName: "webhook-service",
				Namespace:   "namespace",
				CertName:    "cert-name",
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
				ServiceName: "webhook-service",
				Namespace:   "namespace",
				CertName:    "cert-name",
			},
			expectedObj: &corev1.Secret{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			certProvider := certproviders.OpenshiftServiceCaCertificateProvider{}
			require.NoError(t, certProvider.InjectCABundle(tc.obj, tc.cfg))
			require.Equal(t, tc.expectedObj, tc.obj)
		})
	}
}

func Test_OpenshiftServiceCAProvider_AdditionalObjects(t *testing.T) {
	certProvider := certproviders.OpenshiftServiceCaCertificateProvider{}
	objs, err := certProvider.AdditionalObjects(render.CertificateProvisionerConfig{
		ServiceName: "webhook-service",
		Namespace:   "namespace",
		CertName:    "cert-name",
	})
	require.NoError(t, err)
	require.Nil(t, objs)
}

func Test_OpenshiftServiceCAProvider_GetCertSecretInfo(t *testing.T) {
	certProvider := certproviders.OpenshiftServiceCaCertificateProvider{}
	certInfo := certProvider.GetCertSecretInfo(render.CertificateProvisionerConfig{
		ServiceName: "webhook-service",
		Namespace:   "namespace",
		CertName:    "cert-name",
	})
	require.Equal(t, render.CertSecretInfo{
		SecretName:     "cert-name",
		PrivateKeyKey:  "tls.key",
		CertificateKey: "tls.crt",
	}, certInfo)
}
