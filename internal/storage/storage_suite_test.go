package storage

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestStorage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Storage Suite")
}

var (
	testenv *envtest.Environment
	cfg     *rest.Config
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	testenv = &envtest.Environment{}

	var err error
	cfg, err = testenv.Start()
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	Expect(testenv.Stop()).To(Succeed())
})
