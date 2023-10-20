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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var (
	cl  client.Client
	sch *runtime.Scheme
)

// Some of the tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.
// We plan phase Ginkgo out for unit tests.
// See: https://github.com/operator-framework/operator-controller/issues/189
func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

// This setup allows for Ginkgo and standard Go tests to co-exist
// and use the same setup and teardown.
func TestMain(m *testing.M) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	// bootstrapping test environment
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "testdata", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	sch = runtime.NewScheme()
	utilruntime.Must(operatorsv1alpha1.AddToScheme(sch))
	utilruntime.Must(rukpakv1alpha1.AddToScheme(sch))

	cl, err = client.New(cfg, client.Options{Scheme: sch})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	code := m.Run()

	// tearing down the test environment
	err = testEnv.Stop()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	os.Exit(code)
}
