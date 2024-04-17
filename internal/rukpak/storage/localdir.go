package storage

import (
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

	"github.com/operator-framework/operator-controller/internal/rukpak/util"
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
	if err := util.FSToTarGZ(buf, bundle); err != nil {
		return fmt.Errorf("convert bundle %q to tar.gz: %v", owner.GetName(), err)
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
	fsys := &util.FilesOnlyFilesystem{FS: os.DirFS(s.RootDirectory)}
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
