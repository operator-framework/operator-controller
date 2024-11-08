package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogdv1 "github.com/operator-framework/catalogd/api/v1"
)

var (
	cfg        *rest.Config
	c          client.Client
	err        error
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

	sch := scheme.Scheme
	Expect(catalogdv1.AddToScheme(sch)).To(Succeed())
	c, err = client.New(cfg, client.Options{Scheme: sch})
	Expect(err).To(Not(HaveOccurred()))
	kubeClient, err = kubernetes.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())
})
