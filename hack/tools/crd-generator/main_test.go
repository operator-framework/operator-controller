package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunGenerator(t *testing.T) {
	// Get to repo root
	err := os.Chdir("../../..")
	require.NoError(t, err)
	dir, err := os.MkdirTemp("", "crd-generate-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "standard"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "experimental"), 0o700))
	runGenerator(dir, "v0.17.3")

	f1 := filepath.Join(dir, "/standard/olm.operatorframework.io_clusterextensions.yaml")
	f2 := "config/base/operator-controller/crd/standard/olm.operatorframework.io_clusterextensions.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)

	f1 = filepath.Join(dir, "/standard/olm.operatorframework.io_clustercatalogs.yaml")
	f2 = "config/base/catalogd/crd/standard/olm.operatorframework.io_clustercatalogs.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)

	f1 = filepath.Join(dir, "/experimental/olm.operatorframework.io_clusterextensions.yaml")
	f2 = "config/base/operator-controller/crd/experimental/olm.operatorframework.io_clusterextensions.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)

	f1 = filepath.Join(dir, "/experimental/olm.operatorframework.io_clustercatalogs.yaml")
	f2 = "config/base/catalogd/crd/experimental/olm.operatorframework.io_clustercatalogs.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)
}

func compareFiles(t *testing.T, file1, file2 string) {
	f1, err := os.Open(file1)
	require.NoError(t, err)
	defer f1.Close()

	f2, err := os.Open(file2)
	require.NoError(t, err)
	defer f2.Close()

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
