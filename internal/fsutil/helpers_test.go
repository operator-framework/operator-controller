package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/fsutil"
)

func TestEnsureEmptyDirectory(t *testing.T) {
	tempDir := t.TempDir()
	dirPath := filepath.Join(tempDir, "testdir")
	dirPerms := os.FileMode(0755)

	t.Log("Ensure directory is created with the correct perms if it does not already exist")
	require.NoError(t, fsutil.EnsureEmptyDirectory(dirPath, dirPerms))

	stat, err := os.Stat(dirPath)
	require.NoError(t, err)
	require.True(t, stat.IsDir())
	require.Equal(t, dirPerms, stat.Mode().Perm())

	t.Log("Create a file inside directory")
	file := filepath.Join(dirPath, "file1")
	// nolint:gosec
	require.NoError(t, os.WriteFile(file, []byte("test"), 0640))

	t.Log("Create a sub-directory inside directory")
	subDir := filepath.Join(dirPath, "subdir")
	require.NoError(t, os.Mkdir(subDir, dirPerms))

	t.Log("Call EnsureEmptyDirectory against directory with different permissions")
	require.NoError(t, fsutil.EnsureEmptyDirectory(dirPath, 0640))

	t.Log("Ensure directory is now empty")
	entries, err := os.ReadDir(dirPath)
	require.NoError(t, err)
	require.Empty(t, entries)

	t.Log("Ensure original directory permissions are unchanged")
	stat, err = os.Stat(dirPath)
	require.NoError(t, err)
	require.Equal(t, dirPerms, stat.Mode().Perm())
}
