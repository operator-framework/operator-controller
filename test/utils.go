package test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// NewEnv creates a new envtest.Environment instance.
func NewEnv() *envtest.Environment {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			pathFromProjectRoot("helm/olmv1/base/operator-controller/crd/experimental"),
			pathFromProjectRoot("helm/olmv1/base/catalogd/crd/experimental"),
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

// LoadManifests reads a multi-document YAML file (e.g. a rendered Helm manifest) and returns
// all resources matching the given kind. This allows tests to load ValidatingAdmissionPolicy
// and ValidatingAdmissionPolicyBinding resources from the same manifests used in production
// rather than hardcoding them.
func LoadManifests(kind string) ([]*unstructured.Unstructured, error) {
	manifestPath := pathFromProjectRoot("manifests/standard.yaml")
	f, err := os.Open(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", manifestPath, err)
	}
	defer f.Close()

	var result []*unstructured.Unstructured
	decoder := yaml.NewYAMLOrJSONDecoder(bufio.NewReader(f), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decoding manifest: %w", err)
		}
		if obj.GetKind() == kind {
			result = append(result, obj)
		}
	}
	return result, nil
}

// StopWithRetry wraps testEnv.Stop() with retry logic for graceful shutdown.
// This is useful for controller-runtime v0.23.0+ where direct calls to testEnv.Stop()
// can fail intermittently due to graceful shutdown timing.
func StopWithRetry(env interface{ Stop() error }, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := env.Stop(); err == nil {
			return nil
		} else {
			log.Printf("StopWithRetry: env.Stop() failed during teardown, retrying in %s: %v", interval, err)
		}
		time.Sleep(interval)
	}
	err := env.Stop() // Final attempt
	if err != nil {
		log.Printf("StopWithRetry: timeout reached before successful teardown; last error: %v", err)
	}
	return err
}
