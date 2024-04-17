package storage

import (
	"context"
	"io/fs"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Storage interface {
	Loader
	Storer
}

type Loader interface {
	Load(ctx context.Context, owner client.Object) (fs.FS, error)
}

type Storer interface {
	Store(ctx context.Context, owner client.Object, bundle fs.FS) error
	Delete(ctx context.Context, owner client.Object) error

	http.Handler
	URLFor(ctx context.Context, owner client.Object) (string, error)
}

type fallbackLoaderStorage struct {
	Storage
	fallbackLoader Loader
}

func WithFallbackLoader(s Storage, fallback Loader) Storage {
	return &fallbackLoaderStorage{
		Storage:        s,
		fallbackLoader: fallback,
	}
}

func (s *fallbackLoaderStorage) Load(ctx context.Context, owner client.Object) (fs.FS, error) {
	fsys, err := s.Storage.Load(ctx, owner)
	if err != nil {
		return s.fallbackLoader.Load(ctx, owner)
	}
	return fsys, nil
}
