package render_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
	mockrender "github.com/operator-framework/operator-controller/internal/testutil/mock/render"
)

func Test_CertificateProvisioner_WithoutCertProvider(t *testing.T) {
	provisioner := &render.CertificateProvisioner{
		ServiceName:  "webhook",
		CertName:     "cert",
		Namespace:    "namespace",
		CertProvider: nil,
	}

	require.NoError(t, provisioner.InjectCABundle(&corev1.Secret{}))
	require.Nil(t, provisioner.GetCertSecretInfo())

	objs, err := provisioner.AdditionalObjects()
	require.Nil(t, objs)
	require.NoError(t, err)
}

func Test_CertificateProvisioner_WithCertProvider(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCert := mockrender.NewMockCertificateProvider(ctrl)
	mockCert.EXPECT().InjectCABundle(gomock.Any(), gomock.Any()).DoAndReturn(
		func(obj client.Object, cfg render.CertificateProvisionerConfig) error {
			obj.SetName("some-name")
			return nil
		},
	)
	mockCert.EXPECT().AdditionalObjects(gomock.Any()).DoAndReturn(
		func(cfg render.CertificateProvisionerConfig) ([]unstructured.Unstructured, error) {
			return []unstructured.Unstructured{*ToUnstructuredT(t, &corev1.Secret{
				TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: corev1.SchemeGroupVersion.String()},
			})}, nil
		},
	)
	mockCert.EXPECT().GetCertSecretInfo(gomock.Any()).DoAndReturn(
		func(cfg render.CertificateProvisionerConfig) render.CertSecretInfo {
			return render.CertSecretInfo{
				SecretName:     "some-secret",
				PrivateKeyKey:  "some-key",
				CertificateKey: "another-key",
			}
		},
	)
	provisioner := &render.CertificateProvisioner{
		ServiceName:  "webhook",
		CertName:     "cert",
		Namespace:    "namespace",
		CertProvider: mockCert,
	}

	svc := &corev1.Service{}
	require.NoError(t, provisioner.InjectCABundle(svc))
	require.Equal(t, "some-name", svc.GetName())

	objs, err := provisioner.AdditionalObjects()
	require.NoError(t, err)
	require.Equal(t, []unstructured.Unstructured{*ToUnstructuredT(t, &corev1.Secret{
		TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: corev1.SchemeGroupVersion.String()},
	})}, objs)

	require.Equal(t, &render.CertSecretInfo{
		SecretName:     "some-secret",
		PrivateKeyKey:  "some-key",
		CertificateKey: "another-key",
	}, provisioner.GetCertSecretInfo())
}

func Test_CertificateProvisioner_Errors(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCert := mockrender.NewMockCertificateProvider(ctrl)
	mockCert.EXPECT().InjectCABundle(gomock.Any(), gomock.Any()).DoAndReturn(
		func(obj client.Object, cfg render.CertificateProvisionerConfig) error {
			return fmt.Errorf("some error")
		},
	)
	mockCert.EXPECT().AdditionalObjects(gomock.Any()).DoAndReturn(
		func(cfg render.CertificateProvisionerConfig) ([]unstructured.Unstructured, error) {
			return nil, fmt.Errorf("some other error")
		},
	)
	provisioner := &render.CertificateProvisioner{
		ServiceName:  "webhook",
		CertName:     "cert",
		Namespace:    "namespace",
		CertProvider: mockCert,
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
	ctrl := gomock.NewController(t)
	mockCert := mockrender.NewMockCertificateProvider(ctrl)
	prov := render.CertProvisionerFor("my.deployment.thing", render.Options{
		InstallNamespace:    "my-namespace",
		CertificateProvider: mockCert,
	})

	require.Equal(t, prov.CertProvider, mockCert)
	require.Equal(t, "my-deployment-thing-service", prov.ServiceName)
	require.Equal(t, "my-deployment-thing-service-cert", prov.CertName)
	require.Equal(t, "my-namespace", prov.Namespace)
}

func Test_CertProvisionerFor_ExtraLargeName_MoreThan63Chars(t *testing.T) {
	prov := render.CertProvisionerFor("my.object.thing.has.a.really.really.really.really.really.long.name", render.Options{})

	require.Len(t, prov.ServiceName, 63)
	require.Len(t, prov.CertName, 63)
	require.Equal(t, "my-object-thing-has-a-really-really-really-really-reall-service", prov.ServiceName)
	require.Equal(t, "my-object-thing-has-a-really-really-really-really-reall-se-cert", prov.CertName)
}
