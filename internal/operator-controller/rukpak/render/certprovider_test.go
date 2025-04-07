package render_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
)

func Test_CertificateProvisioner_WithoutCertProvider(t *testing.T) {
	provisioner := &render.CertificateProvisioner{
		WebhookServiceName: "webhook",
		CertName:           "cert",
		Namespace:          "namespace",
		CertProvider:       nil,
	}

	require.NoError(t, provisioner.InjectCABundle(&corev1.Secret{}))
	require.Nil(t, provisioner.GetCertSecretInfo())

	objs, err := provisioner.AdditionalObjects()
	require.Nil(t, objs)
	require.NoError(t, err)
}

func Test_CertificateProvisioner_WithCertProvider(t *testing.T) {
	fakeProvider := &FakeCertProvider{
		InjectCABundleFn: func(obj client.Object, cfg render.CertificateProvisionerConfig) error {
			obj.SetName("some-name")
			return nil
		},
		AdditionalObjectsFn: func(cfg render.CertificateProvisionerConfig) ([]unstructured.Unstructured, error) {
			return []unstructured.Unstructured{*ToUnstructuredT(t, &corev1.Secret{})}, nil
		},
		GetCertSecretInfoFn: func(cfg render.CertificateProvisionerConfig) render.CertSecretInfo {
			return render.CertSecretInfo{
				SecretName:     "some-secret",
				PrivateKeyKey:  "some-key",
				CertificateKey: "another-key",
			}
		},
	}
	provisioner := &render.CertificateProvisioner{
		WebhookServiceName: "webhook",
		CertName:           "cert",
		Namespace:          "namespace",
		CertProvider:       fakeProvider,
	}

	svc := &corev1.Service{}
	require.NoError(t, provisioner.InjectCABundle(svc))
	require.Equal(t, "some-name", svc.GetName())

	objs, err := provisioner.AdditionalObjects()
	require.NoError(t, err)
	require.Equal(t, []unstructured.Unstructured{*ToUnstructuredT(t, &corev1.Secret{})}, objs)

	require.Equal(t, &render.CertSecretInfo{
		SecretName:     "some-secret",
		PrivateKeyKey:  "some-key",
		CertificateKey: "another-key",
	}, provisioner.GetCertSecretInfo())
}

func Test_CertificateProvisioner_Errors(t *testing.T) {
	fakeProvider := &FakeCertProvider{
		InjectCABundleFn: func(obj client.Object, cfg render.CertificateProvisionerConfig) error {
			return fmt.Errorf("some error")
		},
		AdditionalObjectsFn: func(cfg render.CertificateProvisionerConfig) ([]unstructured.Unstructured, error) {
			return nil, fmt.Errorf("some other error")
		},
	}
	provisioner := &render.CertificateProvisioner{
		WebhookServiceName: "webhook",
		CertName:           "cert",
		Namespace:          "namespace",
		CertProvider:       fakeProvider,
	}

	err := provisioner.InjectCABundle(&corev1.Service{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "some error")

	objs, err := provisioner.AdditionalObjects()
	require.Error(t, err)
	require.Contains(t, err.Error(), "some other error")
	require.Nil(t, objs)
}

func Test_CertProvisionerFor(t *testing.T) {
	fakeProvider := &FakeCertProvider{}
	prov := render.CertProvisionerFor("my.deployment.thing", render.Options{
		InstallNamespace:    "my-namespace",
		CertificateProvider: fakeProvider,
	})

	require.Equal(t, prov.CertProvider, fakeProvider)
	require.Equal(t, "my-deployment-thing-service", prov.WebhookServiceName)
	require.Equal(t, "my-deployment-thing-service-cert", prov.CertName)
	require.Equal(t, "my-namespace", prov.Namespace)
}

func Test_CertProvisionerFor_ExtraLargeName_MoreThan63Chars(t *testing.T) {
	prov := render.CertProvisionerFor("my.object.thing.has.a.really.really.really.really.really.long.name", render.Options{})

	require.Len(t, prov.WebhookServiceName, 63)
	require.Len(t, prov.CertName, 63)
	require.Equal(t, "my-object-thing-has-a-really-really-really-really-reall-service", prov.WebhookServiceName)
	require.Equal(t, "my-object-thing-has-a-really-really-really-really-reall-se-cert", prov.CertName)
}
