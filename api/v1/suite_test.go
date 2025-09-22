/*
Copyright 2025.

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

package v1

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func newScheme(t *testing.T) *apimachineryruntime.Scheme {
	sch := apimachineryruntime.NewScheme()
	require.NoError(t, AddToScheme(sch))
	return sch
}

func newClient(t *testing.T) client.Client {
	cl, err := client.New(config, client.Options{Scheme: newScheme(t)})
	require.NoError(t, err)
	require.NotNil(t, cl)
	return cl
}

var config *rest.Config

func TestMain(m *testing.M) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "helm", "olmv1", "base", "operator-controller", "crd", "experimental"),
		},
		ErrorIfCRDPathMissing: true,
	}

	// ENVTEST-based tests require specific binaries. By default, these binaries are located
	// in paths defined by controller-runtime. However, the `BinaryAssetsDirectory` needs
	// to be explicitly set when running tests directly (e.g., debugging tests in an IDE)
	// without using the Makefile targets.
	//
	// This is equivalent to configuring your IDE to export the `KUBEBUILDER_ASSETS` environment
	// variable before each test execution. The following function simplifies this process
	// by handling the configuration for you.
	//
	// To ensure the binaries are in the expected path without manual configuration, run:
	// `make envtest-k8s-bins`
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	var err error
	config, err = testEnv.Start()
	utilruntime.Must(err)
	if config == nil {
		log.Panic("expected cfg to not be nil")
	}

	code := m.Run()
	utilruntime.Must(testEnv.Stop())
	os.Exit(code)
}

// getFirstFoundEnvTestBinaryDir finds and returns the first directory under the given path.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "envtest-binaries", "k8s")
	entries, _ := os.ReadDir(basePath)
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
