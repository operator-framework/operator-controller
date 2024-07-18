package storage

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/gzhttp"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

// LocalDir is a storage Instance. When Storing a new FBC contained in
// fs.FS, the content is first written to a temporary file, after which
// it is copied to it's final destination in RootDir/catalogName/. This is
// done so that clients accessing the content stored in RootDir/catalogName have
// atomic view of the content for a catalog.
type LocalDir struct {
	RootDir string
	BaseURL *url.URL
}

func (s LocalDir) Store(ctx context.Context, catalog string, fsys fs.FS) error {
	fbcDir := filepath.Join(s.RootDir, catalog)
	if err := os.MkdirAll(fbcDir, 0700); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(s.RootDir, fmt.Sprint(catalog))
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name())
	err = declcfg.WalkMetasFS(ctx, fsys, func(path string, meta *declcfg.Meta, err error) error {
		if err != nil {
			return fmt.Errorf("error in parsing catalog content files in the filesystem: %w", err)
		}
		_, err = tempFile.Write(meta.Blob)
		return err
	})
	if err != nil {
		return err
	}
	fbcFile := filepath.Join(fbcDir, "all.json")
	return os.Rename(tempFile.Name(), fbcFile)
}

func (s LocalDir) Delete(catalog string) error {
	return os.RemoveAll(filepath.Join(s.RootDir, catalog))
}

func (s LocalDir) ContentURL(catalog string) string {
	return fmt.Sprintf("%s%s/all.json", s.BaseURL, catalog)
}

func (s LocalDir) StorageServerHandler() http.Handler {
	mux := http.NewServeMux()
	fsHandler := http.FileServer(http.FS(&filesOnlyFilesystem{os.DirFS(s.RootDir)}))
	spHandler := http.StripPrefix(s.BaseURL.Path, fsHandler)
	gzHandler := gzhttp.GzipHandler(spHandler)

	typeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/jsonl")
		gzHandler.ServeHTTP(w, r)
	})
	mux.Handle(s.BaseURL.Path, typeHandler)
	return mux
}

func (s LocalDir) ContentExists(catalog string) bool {
	file, err := os.Stat(filepath.Join(s.RootDir, catalog, "all.json"))
	if err != nil {
		return false
	}
	if !file.Mode().IsRegular() {
		// path is not valid content
		return false
	}
	return true
}

// filesOnlyFilesystem is a file system that can open only regular
// files from the underlying filesystem. All other file types result
// in os.ErrNotExists
type filesOnlyFilesystem struct {
	FS fs.FS
}

// Open opens a named file from the underlying filesystem. If the file
// is not a regular file, it return os.ErrNotExists. Callers are resposible
// for closing the file returned.
func (f *filesOnlyFilesystem) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		_ = file.Close()
		return nil, os.ErrNotExist
	}
	return file, nil
}
