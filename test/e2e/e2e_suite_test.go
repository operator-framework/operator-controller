package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

var (
	cfg *rest.Config
	c   client.Client
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
	Expect(err).To(Not(HaveOccurred()))

	err = rukpakv1alpha1.AddToScheme(scheme)
	Expect(err).To(Not(HaveOccurred()))

	c, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).To(Not(HaveOccurred()))
})
