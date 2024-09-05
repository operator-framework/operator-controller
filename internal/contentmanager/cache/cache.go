package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type Watcher interface {
	Watch(source.Source) error
}

// Cache is a storage mechanism for keeping track of
// managed content sources
type Cache interface {
	io.Closer
	// Watch establishes watches for all provided client.Objects.
	// Subsequent calls to Watch will result in no longer necessary
	// watches being stopped and removed and new watches being
	// created
	Watch(context.Context, Watcher, ...client.Object) error
}

// CloserSyncingSource is a wrapper of the controller-runtime
// source.SyncingSource that includes methods for:
//   - Closing the source, stopping it's interaction with the Kubernetes API server and reaction to events
type CloserSyncingSource interface {
	source.SyncingSource
	io.Closer
}

type sourcerer interface {
	// Source returns a CloserSyncingSource for the provided
	// GroupVersionKind. If the CloserSyncingSource encounters an
	// error after having initially synced, it should requeue the
	// provided client.Object and call the provided callback function
	Source(schema.GroupVersionKind, client.Object, func(context.Context)) (CloserSyncingSource, error)
}

type cache struct {
	sources     map[schema.GroupVersionKind]CloserSyncingSource
	sourcerer   sourcerer
	owner       client.Object
	syncTimeout time.Duration
	mu          sync.Mutex
}

func NewCache(sourcerer sourcerer, owner client.Object, syncTimeout time.Duration) Cache {
	return &cache{
		sources:     make(map[schema.GroupVersionKind]CloserSyncingSource),
		sourcerer:   sourcerer,
		owner:       owner,
		syncTimeout: syncTimeout,
	}
}

var _ Cache = (*cache)(nil)

func (c *cache) Watch(ctx context.Context, watcher Watcher, objs ...client.Object) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	gvkSet, err := gvksForObjects(objs...)
	if err != nil {
		return fmt.Errorf("getting set of GVKs for managed objects: %w", err)
	}

	if err := c.removeStaleSources(gvkSet); err != nil {
		return fmt.Errorf("removing stale sources: %w", err)
	}
	return c.startNewSources(ctx, gvkSet, watcher)
}

func (c *cache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	errs := []error{}
	for _, source := range c.sources {
		if err := source.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	slices.SortFunc(errs, func(a, b error) int {
		return strings.Compare(a.Error(), b.Error())
	})

	return errors.Join(errs...)
}

func (c *cache) startNewSources(ctx context.Context, gvks sets.Set[schema.GroupVersionKind], watcher Watcher) error {
	cacheGvks := c.getCacheGVKs()
	gvksToCreate := gvks.Difference(cacheGvks)

	type startResult struct {
		source CloserSyncingSource
		gvk    schema.GroupVersionKind
		err    error
	}
	startResults := make(chan startResult)
	wg := sync.WaitGroup{}
	for _, gvk := range gvksToCreate.UnsortedList() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			source, err := c.startNewSource(ctx, gvk, watcher)
			startResults <- startResult{
				source: source,
				gvk:    gvk,
				err:    err,
			}
		}()
	}
	go func() {
		wg.Wait()
		close(startResults)
	}()

	sourcesErrors := []error{}
	for result := range startResults {
		if result.err != nil {
			sourcesErrors = append(sourcesErrors, result.err)
			continue
		}

		err := c.addSource(result.gvk, result.source)
		if err != nil {
			// If we made it here then there is a logic error in
			// calculating the diffs between what is currently being
			// watched by the cache
			panic(err)
		}
	}

	slices.SortFunc(sourcesErrors, func(a, b error) int {
		return strings.Compare(a.Error(), b.Error())
	})

	return errors.Join(sourcesErrors...)
}

func (c *cache) startNewSource(ctx context.Context, gvk schema.GroupVersionKind, watcher Watcher) (CloserSyncingSource, error) {
	s, err := c.sourcerer.Source(gvk, c.owner, func(ctx context.Context) {
		// this callback function ensures that we remove the source from the
		// cache if it encounters an error after it initially synced successfully
		c.mu.Lock()
		defer c.mu.Unlock()
		err := c.removeSource(gvk)
		if err != nil {
			logr := log.FromContext(ctx)
			logr.Error(err, "managed content cache postSyncError removing source failed", "gvk", gvk)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("getting source: %w", err)
	}

	err = watcher.Watch(s)
	if err != nil {
		return nil, fmt.Errorf("establishing watch for GVK %q: %w", gvk, err)
	}

	syncCtx, syncCancel := context.WithTimeout(ctx, c.syncTimeout)
	defer syncCancel()
	err = s.WaitForSync(syncCtx)
	if err != nil {
		return nil, fmt.Errorf("waiting for sync: %w", err)
	}

	return s, nil
}

func (c *cache) addSource(gvk schema.GroupVersionKind, source CloserSyncingSource) error {
	if _, ok := c.sources[gvk]; !ok {
		c.sources[gvk] = source
		return nil
	}
	return errors.New("source already exists")
}

func (c *cache) removeStaleSources(gvks sets.Set[schema.GroupVersionKind]) error {
	cacheGvks := c.getCacheGVKs()
	removeErrs := []error{}
	gvksToRemove := cacheGvks.Difference(gvks)
	for _, gvk := range gvksToRemove.UnsortedList() {
		err := c.removeSource(gvk)
		if err != nil {
			removeErrs = append(removeErrs, err)
		}
	}

	slices.SortFunc(removeErrs, func(a, b error) int {
		return strings.Compare(a.Error(), b.Error())
	})

	return errors.Join(removeErrs...)
}

func (c *cache) removeSource(gvk schema.GroupVersionKind) error {
	if source, ok := c.sources[gvk]; ok {
		err := source.Close()
		if err != nil {
			return fmt.Errorf("closing source for GVK %q: %w", gvk, err)
		}
	}
	delete(c.sources, gvk)
	return nil
}

func (c *cache) getCacheGVKs() sets.Set[schema.GroupVersionKind] {
	cacheGvks := sets.New[schema.GroupVersionKind]()
	for gvk := range c.sources {
		cacheGvks.Insert(gvk)
	}
	return cacheGvks
}

// gvksForObjects builds a sets.Set of GroupVersionKinds for
// the provided client.Objects. It returns an error if:
//   - There is no Kind set on the client.Object
//   - There is no Version set on the client.Object
//
// An empty Group is assumed to be the "core" Kubernetes
// API group.
func gvksForObjects(objs ...client.Object) (sets.Set[schema.GroupVersionKind], error) {
	gvkSet := sets.New[schema.GroupVersionKind]()
	for _, obj := range objs {
		gvk := obj.GetObjectKind().GroupVersionKind()

		// If the Kind or Version is not set in an object's GroupVersionKind
		// attempting to add it to the runtime.Scheme will result in a panic.
		// To avoid panics, we are doing the validation and returning early
		// with an error if any objects are provided with a missing Kind or Version
		// field
		if gvk.Kind == "" {
			return nil, fmt.Errorf(
				"adding %s to set; object Kind is not defined",
				obj.GetName(),
			)
		}

		if gvk.Version == "" {
			return nil, fmt.Errorf(
				"adding %s to set; object Version is not defined",
				obj.GetName(),
			)
		}

		gvkSet.Insert(gvk)
	}

	return gvkSet, nil
}
