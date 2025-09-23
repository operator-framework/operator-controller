package test

import (
	"os"
	"path"
	"path/filepath"
	"runtime"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// NewEnv creates a new envtest.Environment instance.
func NewEnv() *envtest.Environment {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			pathFromProjectRoot("helm/olmv1/base/operator-controller/crd/experimental"),
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
	return testEnv
}

// pathFromProjectRoot returns the absolute path to the given relative path from the project root.
func pathFromProjectRoot(relativePath string) string {
	_, filename, _, _ := runtime.Caller(0)
	p := path.Join(path.Dir(path.Dir(filename)), relativePath)
	return p
}

// getFirstFoundEnvTestBinaryDir finds and returns the first directory under the given path.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := pathFromProjectRoot(filepath.Join("bin", "envtest-binaries", "k8s"))
	entries, _ := os.ReadDir(basePath)
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
