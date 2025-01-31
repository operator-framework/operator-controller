package source_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/rukpak/source"
)

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
	require.NoError(t, os.Mkdir(nestedDir, source.OwnerWritableDirMode))
	require.NoError(t, os.WriteFile(filePath, []byte("test"), source.OwnerWritableFileMode))
	require.NoError(t, os.Symlink(targetFilePath, symlinkPath))

	t.Log("Set directory structure as read-only")
	require.NoError(t, source.SetReadOnlyRecursive(nestedDir))

	t.Log("Check file permissions")
	stat, err := os.Stat(filePath)
	require.NoError(t, err)
	require.Equal(t, source.OwnerReadOnlyFileMode, stat.Mode().Perm())

	t.Log("Check directory permissions")
	nestedStat, err := os.Stat(nestedDir)
	require.NoError(t, err)
	require.Equal(t, source.OwnerReadOnlyDirMode, nestedStat.Mode().Perm())

	t.Log("Check symlink target file permissions - should not be affected")
	stat, err = os.Stat(targetFilePath)
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0644), stat.Mode().Perm())

	t.Log("Make directory writable to enable test clean-up")
	require.NoError(t, source.SetWritableRecursive(tempDir))
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
	require.NoError(t, os.Mkdir(nestedDir, source.OwnerWritableDirMode))
	require.NoError(t, os.WriteFile(filePath, []byte("test"), source.OwnerReadOnlyFileMode))
	require.NoError(t, os.Symlink(targetFilePath, symlinkPath))

	t.Log("Make directory read-only")
	require.NoError(t, os.Chmod(nestedDir, source.OwnerReadOnlyDirMode))

	t.Log("Call SetWritableRecursive")
	require.NoError(t, source.SetWritableRecursive(nestedDir))

	t.Log("Check file is writable")
	stat, err := os.Stat(filePath)
	require.NoError(t, err)
	require.Equal(t, source.OwnerWritableFileMode, stat.Mode().Perm())

	t.Log("Check directory is writable")
	nestedStat, err := os.Stat(nestedDir)
	require.NoError(t, err)
	require.Equal(t, source.OwnerWritableDirMode, nestedStat.Mode().Perm())

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
	require.NoError(t, os.Mkdir(nestedDir, source.OwnerWritableDirMode))
	require.NoError(t, os.WriteFile(filePath, []byte("test"), source.OwnerReadOnlyFileMode))
	require.NoError(t, os.Chmod(nestedDir, source.OwnerReadOnlyDirMode))

	t.Log("Set directory structure as read-only")
	require.NoError(t, source.DeleteReadOnlyRecursive(nestedDir))

	t.Log("Ensure directory was deleted")
	_, err := os.Stat(nestedDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestIsImageUnpacked(t *testing.T) {
	tempDir := t.TempDir()
	unpackPath := filepath.Join(tempDir, "myimage")

	t.Log("Test case: unpack path does not exist")
	unpacked, modTime, err := source.IsImageUnpacked(unpackPath)
	require.NoError(t, err)
	require.False(t, unpacked)
	require.True(t, modTime.IsZero())

	t.Log("Test case: unpack path points to file")
	require.NoError(t, os.WriteFile(unpackPath, []byte("test"), source.OwnerWritableFileMode))

	unpacked, modTime, err = source.IsImageUnpacked(filepath.Join(tempDir, "myimage"))
	require.NoError(t, err)
	require.False(t, unpacked)
	require.True(t, modTime.IsZero())

	t.Log("Expect file to be deleted")
	_, err = os.Stat(unpackPath)
	require.ErrorIs(t, err, os.ErrNotExist)

	t.Log("Test case: unpack path points to directory (happy path)")
	require.NoError(t, os.Mkdir(unpackPath, source.OwnerWritableDirMode))

	unpacked, modTime, err = source.IsImageUnpacked(unpackPath)
	require.NoError(t, err)
	require.True(t, unpacked)
	require.False(t, modTime.IsZero())

	t.Log("Expect unpack time to match directory mod time")
	stat, err := os.Stat(unpackPath)
	require.NoError(t, err)
	require.Equal(t, stat.ModTime(), modTime)
}
