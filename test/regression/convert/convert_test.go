/*
## registry+v1 bundle regression test

This test in convert_test.go verifies that rendering registry+v1 bundles to manifests
always produces the same files and content.

It runs:     go run generate-manifests.go -output-dir=./testdata/tmp/rendered/
Then compares: ./testdata/tmp/rendered/ vs ./testdata/expected-manifests/

Files are sorted by kind/namespace/name for consistency.

To update expected output (only on purpose), run:

	go run generate-manifests.go -output-dir=./testdata/expected-manifests/
*/
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

// Test_RenderedOutputMatchesExpected runs generate-manifests.go,
// then compares the generated files in ./testdata/tmp/rendered/
// against expected-manifests/.
// It fails if any file differs or is missing.
// TMP dir is cleaned up after test ends.
func Test_RenderedOutput_MatchesExpected(t *testing.T) {
	tmpRoot := "./testdata/tmp/rendered/"
	expectedRoot := "./testdata/expected-manifests/"

	// Remove the temporary output directory always
	t.Cleanup(func() {
		_ = os.RemoveAll("./testdata/tmp")
	})

	// Call the generate-manifests.go script to generate the manifests
	// in the temporary directory.
	cmd := exec.Command("go", "run", "generate-manifests.go", "-output-dir="+tmpRoot)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	err := cmd.Run()
	require.NoError(t, err, "failed to generate manifests")

	// Compare structure + contents
	err = compareDirs(expectedRoot, tmpRoot)
	require.NoError(t, err, "rendered output differs from expected")
}

// compareDirs compares expectedRootPath and generatedRootPath directories recursively.
// It returns an error if any file is missing, extra, or has content mismatch.
// On mismatch, it includes a detailed diff using cmp.Diff.
func compareDirs(expectedRootPath, generatedRootPath string) error {
	// Step 1: Ensure every expected file exists in actual and contents match
	err := filepath.Walk(expectedRootPath, func(expectedPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(expectedRootPath, expectedPath)
		if err != nil {
			return err
		}
		actualPath := filepath.Join(generatedRootPath, relPath)

		expectedBytes, err := os.ReadFile(expectedPath)
		if err != nil {
			return fmt.Errorf("failed to read expected file: %s", expectedPath)
		}
		actualBytes, err := os.ReadFile(actualPath)
		if err != nil {
			return fmt.Errorf("missing file: %s", relPath)
		}

		if !bytes.Equal(expectedBytes, actualBytes) {
			diff := cmp.Diff(string(expectedBytes), string(actualBytes))
			return fmt.Errorf("file content mismatch at: %s\nDiff (-expected +actual):\n%s", relPath, diff)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Step 2: Ensure actual does not contain unexpected files
	err = filepath.Walk(generatedRootPath, func(actualPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(generatedRootPath, actualPath)
		if err != nil {
			return err
		}
		expectedPath := filepath.Join(expectedRootPath, relPath)

		_, err = os.Stat(expectedPath)
		if os.IsNotExist(err) {
			return fmt.Errorf("unexpected extra file: %s", relPath)
		} else if err != nil {
			return fmt.Errorf("error checking expected file: %s", expectedPath)
		}
		return nil
	})
	return err
}
