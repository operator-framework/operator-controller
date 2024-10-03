package cache_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata/cache"
)

const (
	package1 = `{
		"schema": "olm.package",
		"name": "fake1"
	}`

	bundle1 = `{
		"schema": "olm.bundle",
		"name": "fake1.v1.0.0",
		"package": "fake1",
		"image": "fake-image",
		"properties": [
			{
				"type": "olm.package",
				"value": {"packageName":"fake1","version":"1.0.0"}
			}
		]
	}`

	stableChannel = `{
		"schema": "olm.channel",
		"name": "stable",
		"package": "fake1",
		"entries": [
			{
				"name": "fake1.v1.0.0"
			}
		]
	}`
)

func defaultContent() io.Reader {
	return strings.NewReader(package1 + bundle1 + stableChannel)
}

func defaultFS() fstest.MapFS {
	return fstest.MapFS{
		"fake1/olm.package/fake1.json":       &fstest.MapFile{Data: []byte(package1)},
		"fake1/olm.bundle/fake1.v1.0.0.json": &fstest.MapFile{Data: []byte(bundle1)},
		"fake1/olm.channel/stable.json":      &fstest.MapFile{Data: []byte(stableChannel)},
	}
}

func TestFilesystemCachePutAndGet(t *testing.T) {
	const (
		catalogName  = "test-catalog"
		resolvedRef1 = "fake/catalog@sha256:fakesha1"
		resolvedRef2 = "fake/catalog@sha256:fakesha2"
	)

	cacheDir := t.TempDir()
	c := cache.NewFilesystemCache(cacheDir)

	catalogCachePath := filepath.Join(cacheDir, catalogName)
	require.NoDirExists(t, catalogCachePath)

	t.Log("Get empty v1 cache")
	actualFSGet, err := c.Get(catalogName, resolvedRef1)
	assert.NoError(t, err)
	assert.Nil(t, actualFSGet)

	t.Log("Put v1 content into cache")
	actualFSPut, err := c.Put(catalogName, resolvedRef1, defaultContent(), nil)
	assert.NoError(t, err)
	require.NotNil(t, actualFSPut)
	assert.NoError(t, equalFilesystems(defaultFS(), actualFSPut))

	t.Log("Get v1 content from cache")
	actualFSGet, err = c.Get(catalogName, resolvedRef1)
	assert.NoError(t, err)
	require.NotNil(t, actualFSGet)
	assert.NoError(t, equalFilesystems(defaultFS(), actualFSPut))
	assert.NoError(t, equalFilesystems(actualFSPut, actualFSGet))

	t.Log("Put v1 error into cache")
	actualFSPut, err = c.Put(catalogName, resolvedRef1, nil, errors.New("fake put error"))
	// Errors do not override previously successfully populated cache
	assert.NoError(t, err)
	require.NotNil(t, actualFSPut)
	assert.NoError(t, equalFilesystems(defaultFS(), actualFSPut))
	assert.NoError(t, equalFilesystems(actualFSPut, actualFSGet))

	t.Log("Put v2 error into cache")
	actualFSPut, err = c.Put(catalogName, resolvedRef2, nil, errors.New("fake v2 put error"))
	assert.Equal(t, errors.New("fake v2 put error"), err)
	assert.Nil(t, actualFSPut)

	t.Log("Get v2 error from cache")
	actualFSGet, err = c.Get(catalogName, resolvedRef2)
	assert.Equal(t, errors.New("fake v2 put error"), err)
	assert.Nil(t, actualFSGet)

	t.Log("Put v2 content into cache")
	actualFSPut, err = c.Put(catalogName, resolvedRef2, defaultContent(), nil)
	assert.NoError(t, err)
	require.NotNil(t, actualFSPut)
	assert.NoError(t, equalFilesystems(defaultFS(), actualFSPut))

	t.Log("Get v2 content from cache")
	actualFSGet, err = c.Get(catalogName, resolvedRef2)
	assert.NoError(t, err)
	require.NotNil(t, actualFSGet)
	assert.NoError(t, equalFilesystems(defaultFS(), actualFSPut))
	assert.NoError(t, equalFilesystems(actualFSPut, actualFSGet))

	t.Log("Get empty v1 cache")
	// Cache should be empty and no error because
	// Put with a new version overrides the old version
	actualFSGet, err = c.Get(catalogName, resolvedRef1)
	assert.NoError(t, err)
	assert.Nil(t, actualFSGet)
}

func TestFilesystemCacheRemove(t *testing.T) {
	catalogName := "test-catalog"
	resolvedRef := "fake/catalog@sha256:fakesha"

	cacheDir := t.TempDir()
	c := cache.NewFilesystemCache(cacheDir)

	catalogCachePath := filepath.Join(cacheDir, catalogName)

	t.Log("Remove cache before it exists")
	require.NoDirExists(t, catalogCachePath)
	err := c.Remove(catalogName)
	require.NoError(t, err)
	assert.NoDirExists(t, catalogCachePath)

	t.Log("Fetch contents to populate cache")
	_, err = c.Put(catalogName, resolvedRef, defaultContent(), nil)
	require.NoError(t, err)
	require.DirExists(t, catalogCachePath)

	t.Log("Temporary change permissions to the cache dir to cause error")
	require.NoError(t, os.Chmod(catalogCachePath, 0000))

	t.Log("Remove cache causes an error")
	err = c.Remove(catalogName)
	require.ErrorContains(t, err, "error removing cache directory")
	require.DirExists(t, catalogCachePath)

	t.Log("Restore directory permissions for successful removal")
	require.NoError(t, os.Chmod(catalogCachePath, 0777))

	t.Log("Remove cache")
	err = c.Remove(catalogName)
	require.NoError(t, err)
	assert.NoDirExists(t, catalogCachePath)
}

func equalFilesystems(expected, actual fs.FS) error {
	normalizeJSON := func(data []byte) []byte {
		var v interface{}
		if err := json.Unmarshal(data, &v); err != nil {
			return data
		}
		norm, err := json.Marshal(v)
		if err != nil {
			return data
		}
		return norm
	}
	compare := func(expected, actual fs.FS, path string) error {
		expectedData, expectedErr := fs.ReadFile(expected, path)
		actualData, actualErr := fs.ReadFile(actual, path)

		switch {
		case expectedErr == nil && actualErr != nil:
			return fmt.Errorf("path %q: read error in actual FS: %v", path, actualErr)
		case expectedErr != nil && actualErr == nil:
			return fmt.Errorf("path %q: read error in expected FS: %v", path, expectedErr)
		case expectedErr != nil && actualErr != nil && expectedErr.Error() != actualErr.Error():
			return fmt.Errorf("path %q: different read errors: expected: %v, actual: %v", path, expectedErr, actualErr)
		}

		if filepath.Ext(path) == ".json" {
			expectedData = normalizeJSON(expectedData)
			actualData = normalizeJSON(actualData)
		}

		if !bytes.Equal(expectedData, actualData) {
			return fmt.Errorf("path %q: file contents do not match: %s", path, cmp.Diff(string(expectedData), string(actualData)))
		}
		return nil
	}

	paths := sets.New[string]()
	for _, fsys := range []fs.FS{expected, actual} {
		if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			paths.Insert(path)
			return nil
		}); err != nil {
			return err
		}
	}

	var cmpErrs []error
	for _, path := range sets.List(paths) {
		if err := compare(expected, actual, path); err != nil {
			cmpErrs = append(cmpErrs, err)
		}
	}
	return errors.Join(cmpErrs...)
}
