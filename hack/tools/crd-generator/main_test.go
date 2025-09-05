package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const controllerToolsVersion = "v0.19.0"

func TestRunGenerator(t *testing.T) {
	here, err := os.Getwd()
	require.NoError(t, err)
	// Get to repo root
	err = os.Chdir("../../..")
	require.NoError(t, err)
	defer func() {
		_ = os.Chdir(here)
	}()
	dir, err := os.MkdirTemp("", "crd-generate-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "standard"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "experimental"), 0o700))
	runGenerator(dir, controllerToolsVersion)

	f1 := filepath.Join(dir, "standard/olm.operatorframework.io_clusterextensions.yaml")
	f2 := "config/base/operator-controller/crd/standard/olm.operatorframework.io_clusterextensions.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)

	f1 = filepath.Join(dir, "standard/olm.operatorframework.io_clustercatalogs.yaml")
	f2 = "config/base/catalogd/crd/standard/olm.operatorframework.io_clustercatalogs.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)

	f1 = filepath.Join(dir, "experimental/olm.operatorframework.io_clusterextensions.yaml")
	f2 = "config/base/operator-controller/crd/experimental/olm.operatorframework.io_clusterextensions.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)

	f1 = filepath.Join(dir, "experimental/olm.operatorframework.io_clustercatalogs.yaml")
	f2 = "config/base/catalogd/crd/experimental/olm.operatorframework.io_clustercatalogs.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)
}

func TestTags(t *testing.T) {
	here, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir("testdata")
	defer func() {
		_ = os.Chdir(here)
	}()
	require.NoError(t, err)
	dir, err := os.MkdirTemp("", "crd-generate-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "standard"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "experimental"), 0o700))
	runGenerator(dir, controllerToolsVersion, "github.com/operator-framework/operator-controller/hack/tools/crd-generator/testdata/api/v1")

	f1 := filepath.Join(dir, "standard/olm.operatorframework.io_clusterextensions.yaml")
	f2 := "output/standard/olm.operatorframework.io_clusterextensions.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)

	f1 = filepath.Join(dir, "experimental/olm.operatorframework.io_clusterextensions.yaml")
	f2 = "output/experimental/olm.operatorframework.io_clusterextensions.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)
}

func compareFiles(t *testing.T, file1, file2 string) {
	f1, err := os.Open(file1)
	require.NoError(t, err)
	defer func() {
		_ = f1.Close()
	}()

	f2, err := os.Open(file2)
	require.NoError(t, err)
	defer func() {
		_ = f2.Close()
	}()

	for {
		b1 := make([]byte, 64000)
		b2 := make([]byte, 64000)
		n1, err1 := f1.Read(b1)
		n2, err2 := f2.Read(b2)

		// Success if both have EOF at the same time
		if err1 == io.EOF && err2 == io.EOF {
			return
		}
		require.NoError(t, err1)
		require.NoError(t, err2)
		require.Equal(t, n1, n2)
		require.Equal(t, b1, b2)
	}
}
