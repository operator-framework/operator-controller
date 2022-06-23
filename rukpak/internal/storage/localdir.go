package storage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/nlepage/go-tarfs"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Storage = &LocalDirectory{}

const DefaultBundleCacheDir = "/var/cache/bundles"

type LocalDirectory struct {
	RootDirectory string
	URL           url.URL
}

func (s *LocalDirectory) Load(_ context.Context, owner client.Object) (fs.FS, error) {
	bundleFile, err := os.Open(s.bundlePath(owner.GetName()))
	if err != nil {
		return nil, err
	}
	defer bundleFile.Close()
	tarReader, err := gzip.NewReader(bundleFile)
	if err != nil {
		return nil, err
	}
	return tarfs.New(tarReader)
}

func (s *LocalDirectory) Store(_ context.Context, owner client.Object, bundle fs.FS) error {
	buf := &bytes.Buffer{}
	gzw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gzw)
	if err := fs.WalkDir(bundle, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("get file info for %q: %v", path, err)
		}

		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("build tar file info header for %q: %v", path, err)
		}
		h.Uid = 0
		h.Gid = 0
		h.Uname = ""
		h.Gname = ""
		h.Name = path

		if err := tw.WriteHeader(h); err != nil {
			return fmt.Errorf("write tar header for %q: %v", path, err)
		}
		if d.IsDir() {
			return nil
		}
		f, err := bundle.Open(path)
		if err != nil {
			return fmt.Errorf("open file %q: %v", path, err)
		}
		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("write tar data for %q: %v", path, err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("generate tar.gz for bundle %q: %v", owner.GetName(), err)
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gzw.Close(); err != nil {
		return err
	}

	bundleFile, err := os.Create(s.bundlePath(owner.GetName()))
	if err != nil {
		return err
	}
	defer bundleFile.Close()

	if _, err := io.Copy(bundleFile, buf); err != nil {
		return err
	}
	return nil
}

func (s *LocalDirectory) Delete(_ context.Context, owner client.Object) error {
	return ignoreNotExist(os.Remove(s.bundlePath(owner.GetName())))
}

func (s *LocalDirectory) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	fsys := &filesOnlyFilesystem{os.DirFS(s.RootDirectory)}
	http.StripPrefix(s.URL.Path, http.FileServer(http.FS(fsys))).ServeHTTP(resp, req)
}

func (s *LocalDirectory) URLFor(_ context.Context, owner client.Object) (string, error) {
	return fmt.Sprintf("%s%s", s.URL.String(), localDirectoryBundleFile(owner.GetName())), nil
}

func (s *LocalDirectory) bundlePath(bundleName string) string {
	return filepath.Join(s.RootDirectory, localDirectoryBundleFile(bundleName))
}

func localDirectoryBundleFile(bundleName string) string {
	return fmt.Sprintf("%s.tgz", bundleName)
}

func ignoreNotExist(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// filesOnlyFilesystem is an fs.FS implementation that treats non-regular files
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
type filesOnlyFilesystem struct {
	fs fs.FS
}

func (f *filesOnlyFilesystem) Open(name string) (fs.File, error) {
	file, err := f.fs.Open(name)
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
