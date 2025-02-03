package source_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/rukpak/source"
)

func TestIsImageUnpacked(t *testing.T) {
	tempDir := t.TempDir()
	unpackPath := filepath.Join(tempDir, "myimage")

	t.Log("Test case: unpack path does not exist")
	unpacked, modTime, err := source.IsImageUnpacked(unpackPath)
	require.NoError(t, err)
	require.False(t, unpacked)
	require.True(t, modTime.IsZero())

	t.Log("Test case: unpack path points to file")
	require.NoError(t, os.WriteFile(unpackPath, []byte("test"), 0600))

	unpacked, modTime, err = source.IsImageUnpacked(filepath.Join(tempDir, "myimage"))
	require.NoError(t, err)
	require.False(t, unpacked)
	require.True(t, modTime.IsZero())

	t.Log("Expect file to be deleted")
	_, err = os.Stat(unpackPath)
	require.ErrorIs(t, err, os.ErrNotExist)

	t.Log("Test case: unpack path points to directory (happy path)")
	require.NoError(t, os.Mkdir(unpackPath, 0700))

	unpacked, modTime, err = source.IsImageUnpacked(unpackPath)
	require.NoError(t, err)
	require.True(t, unpacked)
	require.False(t, modTime.IsZero())

	t.Log("Expect unpack time to match directory mod time")
	stat, err := os.Stat(unpackPath)
	require.NoError(t, err)
	require.Equal(t, stat.ModTime(), modTime)
}
