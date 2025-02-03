package fsutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureEmptyDirectory(t *testing.T) {
	tempDir := t.TempDir()
	dirPath := filepath.Join(tempDir, "testdir")
	dirPerms := os.FileMode(0755)

	t.Log("Ensure directory is created with the correct perms if it does not already exist")
	require.NoError(t, EnsureEmptyDirectory(dirPath, dirPerms))

	stat, err := os.Stat(dirPath)
	require.NoError(t, err)
	require.True(t, stat.IsDir())
	require.Equal(t, dirPerms, stat.Mode().Perm())

	t.Log("Create a file inside directory")
	file := filepath.Join(dirPath, "file1")
	// write file as read-only to verify EnsureEmptyDirectory can still delete it.
	require.NoError(t, os.WriteFile(file, []byte("test"), 0400))

	t.Log("Create a sub-directory inside directory")
	subDir := filepath.Join(dirPath, "subdir")
	// write subDir as read-execute-only to verify EnsureEmptyDirectory can still delete it.
	require.NoError(t, os.Mkdir(subDir, 0500))

	t.Log("Call EnsureEmptyDirectory against directory with different permissions")
	require.NoError(t, EnsureEmptyDirectory(dirPath, 0640))

	t.Log("Ensure directory is now empty")
	entries, err := os.ReadDir(dirPath)
	require.NoError(t, err)
	require.Empty(t, entries)

	t.Log("Ensure original directory permissions are unchanged")
	stat, err = os.Stat(dirPath)
	require.NoError(t, err)
	require.Equal(t, dirPerms, stat.Mode().Perm())
}

func TestSetReadOnlyRecursive(t *testing.T) {
	tempDir := t.TempDir()
	targetFilePath := filepath.Join(tempDir, "target")
	nestedDir := filepath.Join(tempDir, "nested")
	filePath := filepath.Join(nestedDir, "testfile")
	symlinkPath := filepath.Join(nestedDir, "symlink")

	t.Log("Create symlink target file outside directory with its own permissions")
	// nolint:gosec
	require.NoError(t, os.WriteFile(targetFilePath, []byte("something"), 0644))

	t.Log("Create a nested directory structure that contains a file and sym. link")
	require.NoError(t, os.Mkdir(nestedDir, ownerWritableDirMode))
	require.NoError(t, os.WriteFile(filePath, []byte("test"), ownerWritableFileMode))
	require.NoError(t, os.Symlink(targetFilePath, symlinkPath))

	t.Log("Set directory structure as read-only")
	require.NoError(t, SetReadOnlyRecursive(nestedDir))

	t.Log("Check file permissions")
	stat, err := os.Stat(filePath)
	require.NoError(t, err)
	require.Equal(t, ownerReadOnlyFileMode, stat.Mode().Perm())

	t.Log("Check directory permissions")
	nestedStat, err := os.Stat(nestedDir)
	require.NoError(t, err)
	require.Equal(t, ownerReadOnlyDirMode, nestedStat.Mode().Perm())

	t.Log("Check symlink target file permissions - should not be affected")
	stat, err = os.Stat(targetFilePath)
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0644), stat.Mode().Perm())

	t.Log("Make directory writable to enable test clean-up")
	require.NoError(t, SetWritableRecursive(tempDir))
}

func TestSetWritableRecursive(t *testing.T) {
	tempDir := t.TempDir()
	targetFilePath := filepath.Join(tempDir, "target")
	nestedDir := filepath.Join(tempDir, "nested")
	filePath := filepath.Join(nestedDir, "testfile")
	symlinkPath := filepath.Join(nestedDir, "symlink")

	t.Log("Create symlink target file outside directory with its own permissions")
	// nolint:gosec
	require.NoError(t, os.WriteFile(targetFilePath, []byte("something"), 0644))

	t.Log("Create a nested directory (writable) structure that contains a file (read-only) and sym. link")
	require.NoError(t, os.Mkdir(nestedDir, ownerWritableDirMode))
	require.NoError(t, os.WriteFile(filePath, []byte("test"), ownerReadOnlyFileMode))
	require.NoError(t, os.Symlink(targetFilePath, symlinkPath))

	t.Log("Make directory read-only")
	require.NoError(t, os.Chmod(nestedDir, ownerReadOnlyDirMode))

	t.Log("Call SetWritableRecursive")
	require.NoError(t, SetWritableRecursive(nestedDir))

	t.Log("Check file is writable")
	stat, err := os.Stat(filePath)
	require.NoError(t, err)
	require.Equal(t, ownerWritableFileMode, stat.Mode().Perm())

	t.Log("Check directory is writable")
	nestedStat, err := os.Stat(nestedDir)
	require.NoError(t, err)
	require.Equal(t, ownerWritableDirMode, nestedStat.Mode().Perm())

	t.Log("Check symlink target file permissions - should not be affected")
	stat, err = os.Stat(targetFilePath)
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0644), stat.Mode().Perm())
}

func TestDeleteReadOnlyRecursive(t *testing.T) {
	tempDir := t.TempDir()
	nestedDir := filepath.Join(tempDir, "nested")
	filePath := filepath.Join(nestedDir, "testfile")

	t.Log("Create a nested read-only directory structure that contains a file and sym. link")
	require.NoError(t, os.Mkdir(nestedDir, ownerWritableDirMode))
	require.NoError(t, os.WriteFile(filePath, []byte("test"), ownerReadOnlyFileMode))
	require.NoError(t, os.Chmod(nestedDir, ownerReadOnlyDirMode))

	t.Log("Set directory structure as read-only via DeleteReadOnlyRecursive")
	require.NoError(t, DeleteReadOnlyRecursive(nestedDir))

	t.Log("Ensure directory was deleted")
	_, err := os.Stat(nestedDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}
