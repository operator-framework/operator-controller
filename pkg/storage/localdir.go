package storage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

// LocalDir is a storage Instance. When Storing a new FBC contained in
// fs.FS, the content is first written to a temporary file, after which
// it is copied to it's final destination in RootDir/catalogName/. This is
// done so that clients accessing the content stored in RootDir/catalogName have
// atomic view of the content for a catalog.
type LocalDir struct {
	RootDir string
}

func (s LocalDir) Store(catalog string, fsys fs.FS) error {
	fbcDir := filepath.Join(s.RootDir, catalog)
	if err := os.MkdirAll(fbcDir, 0700); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(s.RootDir, fmt.Sprint(catalog))
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name())
	err = declcfg.WalkMetasFS(fsys, func(path string, meta *declcfg.Meta, err error) error {
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
