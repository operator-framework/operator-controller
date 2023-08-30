package storage

import "io/fs"

// Instance is a storage instance that stores FBC content of catalogs
// added to a cluster. It can be used to Store or Delete FBC in the
// host's filesystem
type Instance interface {
	Store(catalog string, fsys fs.FS) error
	Delete(catalog string) error
}
