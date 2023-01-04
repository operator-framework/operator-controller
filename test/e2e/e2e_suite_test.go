package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var (
	cfg        *rest.Config
	c          client.Client
	kubeClient kubernetes.Interface
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	SetDefaultEventuallyTimeout(1 * time.Minute)
	SetDefaultEventuallyPollingInterval(1 * time.Second)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
	cfg = ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()
	err := operatorv1alpha1.AddToScheme(scheme)
	Expect(err).To(BeNil())
	err = rbacv1.AddToScheme(scheme)
	Expect(err).To(BeNil())
	err = batchv1.AddToScheme(scheme)
	Expect(err).To(BeNil())

	err = corev1.AddToScheme(scheme)
	Expect(err).To(BeNil())

	err = appsv1.AddToScheme(scheme)
	Expect(err).To(BeNil())

	err = apiextensionsv1.AddToScheme(scheme)
	Expect(err).To(BeNil())

	c, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).To(BeNil())

	kubeClient, err = kubernetes.NewForConfig(cfg)
	Expect(err).To(BeNil())
})
