package garbagecollection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/metadata"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	catalogdv1 "github.com/operator-framework/catalogd/api/v1"
)

var _ manager.Runnable = (*GarbageCollector)(nil)

// GarbageCollector is an implementation of the manager.Runnable
// interface for running garbage collection on the Catalog content
// cache that is served by the catalogd HTTP server. It runs in a loop
// and will ensure that no cache entries exist for Catalog resources
// that no longer exist. This should only clean up cache entries that
// were missed by the handling of a DELETE event on a Catalog resource.
type GarbageCollector struct {
	CachePath      string
	Logger         logr.Logger
	MetadataClient metadata.Interface
	Interval       time.Duration
}

// Start will start the garbage collector. It will always run once on startup
// and loop until context is canceled after an initial garbage collection run.
// Garbage collection will run again every X amount of time, where X is the
// supplied garbage collection interval.
func (gc *GarbageCollector) Start(ctx context.Context) error {
	// Run once on startup
	removed, err := runGarbageCollection(ctx, gc.CachePath, gc.MetadataClient)
	if err != nil {
		gc.Logger.Error(err, "running garbage collection")
	}
	if len(removed) > 0 {
		gc.Logger.Info("removed stale cache entries", "removed entries", removed)
	}

	// Loop until context is canceled, running garbage collection
	// at the configured interval
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(gc.Interval):
			removed, err := runGarbageCollection(ctx, gc.CachePath, gc.MetadataClient)
			if err != nil {
				gc.Logger.Error(err, "running garbage collection")
			}
			if len(removed) > 0 {
				gc.Logger.Info("removed stale cache entries", "removed entries", removed)
			}
		}
	}
}

func runGarbageCollection(ctx context.Context, cachePath string, metaClient metadata.Interface) ([]string, error) {
	getter := metaClient.Resource(catalogdv1.GroupVersion.WithResource("clustercatalogs"))
	metaList, err := getter.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing clustercatalogs: %w", err)
	}

	expectedCatalogs := sets.New[string]()
	for _, meta := range metaList.Items {
		expectedCatalogs.Insert(meta.GetName())
	}

	cacheDirEntries, err := os.ReadDir(cachePath)
	if err != nil {
		return nil, fmt.Errorf("error reading cache directory: %w", err)
	}
	removed := []string{}
	for _, cacheDirEntry := range cacheDirEntries {
		if cacheDirEntry.IsDir() && expectedCatalogs.Has(cacheDirEntry.Name()) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(cachePath, cacheDirEntry.Name())); err != nil {
			return nil, fmt.Errorf("error removing cache directory entry %q: %w  ", cacheDirEntry.Name(), err)
		}

		removed = append(removed, cacheDirEntry.Name())
	}
	return removed, nil
}
