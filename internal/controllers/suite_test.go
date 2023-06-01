/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers_test

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	catalogd "github.com/operator-framework/catalogd/pkg/apis/core/v1beta1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg     *rest.Config
	cl      client.Client
	sch     *runtime.Scheme
	testEnv *envtest.Environment
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "testdata", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	sch = runtime.NewScheme()
	err = operatorsv1alpha1.AddToScheme(sch)
	Expect(err).NotTo(HaveOccurred())
	err = rukpakv1alpha1.AddToScheme(sch)
	Expect(err).NotTo(HaveOccurred())
	err = catalogd.AddToScheme(sch)
	Expect(err).NotTo(HaveOccurred())

	cl, err = client.New(cfg, client.Options{Scheme: sch})
	Expect(err).NotTo(HaveOccurred())
	Expect(cl).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	var operators operatorsv1alpha1.OperatorList
	var bundleDeployments rukpakv1alpha1.BundleDeploymentList

	Expect(cl.List(context.Background(), &operators)).To(Succeed())
	Expect(cl.List(context.Background(), &bundleDeployments)).To(Succeed())

	Expect(namesFromList(&operators)).To(BeEmpty(), "operators left in the cluster")
	Expect(namesFromList(&bundleDeployments)).To(BeEmpty(), "bundle deployments left in the cluster")

	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func namesFromList(list client.ObjectList) []string {
	var names []string

	items, err := meta.ExtractList(list)
	Expect(err).NotTo(HaveOccurred())
	for _, item := range items {
		names = append(names, item.(client.Object).GetName())
	}
	return names
}
