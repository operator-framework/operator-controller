package storage

import (
	"context"
	"io/fs"
)

type Storage interface {
	Loader
	Storer
}

type Loader interface {
	Load(ctx context.Context, owner string) (fs.FS, error)
}

type Storer interface {
	Store(ctx context.Context, owner string, bundle fs.FS) error
	Delete(ctx context.Context, owner string) error
}
