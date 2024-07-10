package util

import (
	"io/fs"
	"os"
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
