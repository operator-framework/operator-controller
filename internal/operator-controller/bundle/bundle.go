package bundle

import "io/fs"

type Bundle interface {
	FS() fs.FS
}

type fsBundle struct {
	bundleFS fs.FS
}

func (f fsBundle) FS() fs.FS {
	return f.bundleFS
}

func FromFS(bundleFS fs.FS) Bundle {
	return fsBundle{bundleFS}
}
