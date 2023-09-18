package storage

import (
	"io/fs"
	"net/http"
)

// Instance is a storage instance that stores FBC content of catalogs
// added to a cluster. It can be used to Store or Delete FBC in the
// host's filesystem. It also a manager runnable object, that starts
// a server to serve the content stored.
type Instance interface {
	Store(catalog string, fsys fs.FS) error
	Delete(catalog string) error
	ContentURL(catalog string) string
	StorageServerHandler() http.Handler
}
