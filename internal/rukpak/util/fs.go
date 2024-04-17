package util

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing/fstest"
)

// FilesOnlyFilesystem is an fs.FS implementation that treats non-regular files
// (e.g. directories, symlinks, devices, etc.) as non-existent. The reason for
// this is so that we only serve bundle files.
//
// This treats directories as not found so that the http server does not serve
// HTML directory index responses.
//
// This treats other symlink files as not found so that we prevent HTTP requests
// from escaping the filesystem root.
//
// Lastly, this treats other non-regular files as not found because they are
// out of scope for serving bundle contents.
type FilesOnlyFilesystem struct {
	FS fs.FS
}

func (f *FilesOnlyFilesystem) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, os.ErrNotExist
	}
	return file, nil
}

// EnsureBaseDirFS ensures that an fs.FS contains a single directory in its root
// This is useful for bundle formats that require a base directory in the root of
// the bundle.
//
// For example, an unpacked Helm chart might have <chartDir>/Chart.yaml, and we'd
// typically assume <chartDir> as the bundle root. However, when helm archives
// contain <chartDir> at the root of the archive: <archiveRoot>/<chartDir>/Chart.yaml.
//
// If the fs.FS already has this structure, EnsureBaseDirFS just returns fsys
// directly. Otherwise, it returns a new fs.FS where the defaultBaseDir is inserted
// at the root, such that fsys appears within defaultBaseDir.
func EnsureBaseDirFS(fsys fs.FS, defaultBaseDir string) (fs.FS, error) {
	cleanDefaultBaseDir := filepath.Clean(defaultBaseDir)
	if dir, _ := filepath.Split(cleanDefaultBaseDir); dir != "" {
		return nil, fmt.Errorf("default base directory %q contains multiple path segments: must be exactly one", defaultBaseDir)
	}
	rootFSEntries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	if len(rootFSEntries) == 1 && rootFSEntries[0].IsDir() {
		return fsys, nil
	}
	return &baseDirFS{fsys, defaultBaseDir}, nil
}

type baseDirFS struct {
	fsys    fs.FS
	baseDir string
}

func (f baseDirFS) Open(name string) (fs.File, error) {
	if name == "." {
		return fstest.MapFS{f.baseDir: &fstest.MapFile{Mode: fs.ModeDir}}.Open(name)
	}
	if name == f.baseDir {
		return f.fsys.Open(".")
	}
	basePrefix := f.baseDir + string(os.PathSeparator)
	if strings.HasPrefix(name, basePrefix) {
		subName := strings.TrimPrefix(name, basePrefix)
		if subName == "" {
			subName = "."
		}
		return f.fsys.Open(subName)
	}
	return nil, fs.ErrNotExist
}
