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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
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
	// Source returns a CloserSyncingSource for the provided namespace
	// and GroupVersionKind. If the CloserSyncingSource encounters an
	// error after having initially synced, it should requeue the
	// provided client.Object and call the provided callback function
	Source(string, schema.GroupVersionKind, client.Object, func(context.Context)) (CloserSyncingSource, error)
}

type cache struct {
	sources     map[sourceKey]CloserSyncingSource
	sourcerer   sourcerer
	owner       client.Object
	syncTimeout time.Duration
	mu          sync.Mutex
	restMapper  meta.RESTMapper
}

func NewCache(sourcerer sourcerer, owner client.Object, syncTimeout time.Duration, rm meta.RESTMapper) Cache {
	return &cache{
		sources:     make(map[sourceKey]CloserSyncingSource),
		sourcerer:   sourcerer,
		owner:       owner,
		syncTimeout: syncTimeout,
		restMapper:  rm,
	}
}

var _ Cache = (*cache)(nil)

func (c *cache) Watch(ctx context.Context, watcher Watcher, objs ...client.Object) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	sourceKeySet, err := c.sourceKeysForObjects(objs...)
	if err != nil {
		return fmt.Errorf("getting set of GVKs for managed objects: %w", err)
	}

	if err := c.removeStaleSources(sourceKeySet); err != nil {
		return fmt.Errorf("removing stale sources: %w", err)
	}
	return c.startNewSources(ctx, sourceKeySet, watcher)
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

type sourceKey struct {
	namespace string
	gvk       schema.GroupVersionKind
}

func (c *cache) startNewSources(ctx context.Context, sources sets.Set[sourceKey], watcher Watcher) error {
	type startResult struct {
		source CloserSyncingSource
		key    sourceKey
		err    error
	}
	startResults := make(chan startResult)
	wg := sync.WaitGroup{}

	existingSourceKeys := c.getCacheKeys()
	sourcesToCreate := sources.Difference(existingSourceKeys)
	for _, srcKey := range sourcesToCreate.UnsortedList() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			source, err := c.startNewSource(ctx, srcKey, watcher)
			startResults <- startResult{
				source: source,
				key:    srcKey,
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

		err := c.addSource(result.key, result.source)
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

func (c *cache) startNewSource(ctx context.Context, srcKey sourceKey, watcher Watcher) (CloserSyncingSource, error) {
	s, err := c.sourcerer.Source(srcKey.namespace, srcKey.gvk, c.owner, func(ctx context.Context) {
		// this callback function ensures that we remove the source from the
		// cache if it encounters an error after it initially synced successfully
		c.mu.Lock()
		defer c.mu.Unlock()
		err := c.removeSource(srcKey)
		if err != nil {
			logr := log.FromContext(ctx)
			logr.Error(err, "managed content cache postSyncError removing source failed", "namespace", srcKey.namespace, "gvk", srcKey.gvk)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("getting source: %w", err)
	}

	err = watcher.Watch(s)
	if err != nil {
		return nil, fmt.Errorf("establishing watch for GVK %q in namespace %q: %w", srcKey.gvk, srcKey.namespace, err)
	}

	syncCtx, syncCancel := context.WithTimeout(ctx, c.syncTimeout)
	defer syncCancel()
	err = s.WaitForSync(syncCtx)
	if err != nil {
		return nil, fmt.Errorf("waiting for sync: %w", err)
	}

	return s, nil
}

func (c *cache) addSource(key sourceKey, source CloserSyncingSource) error {
	if _, ok := c.sources[key]; !ok {
		c.sources[key] = source
		return nil
	}
	return errors.New("source already exists")
}

func (c *cache) removeStaleSources(srcKeys sets.Set[sourceKey]) error {
	existingSrcKeys := c.getCacheKeys()
	removeErrs := []error{}
	srcKeysToRemove := existingSrcKeys.Difference(srcKeys)
	for _, gvk := range srcKeysToRemove.UnsortedList() {
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

func (c *cache) removeSource(srcKey sourceKey) error {
	if src, ok := c.sources[srcKey]; ok {
		err := src.Close()
		if err != nil {
			return fmt.Errorf("closing source for GVK %q in namespace %q: %w", srcKey.gvk, srcKey.namespace, err)
		}
	}
	delete(c.sources, srcKey)
	return nil
}

func (c *cache) getCacheKeys() sets.Set[sourceKey] {
	sourceKeys := sets.New[sourceKey]()
	for key := range c.sources {
		sourceKeys.Insert(key)
	}
	return sourceKeys
}

// gvksForObjects builds a sets.Set of GroupVersionKinds for
// the provided client.Objects. It returns an error if:
//   - There is no Kind set on the client.Object
//   - There is no Version set on the client.Object
//
// An empty Group is assumed to be the "core" Kubernetes
// API group.
func (c *cache) sourceKeysForObjects(objs ...client.Object) (sets.Set[sourceKey], error) {
	sourceKeys := sets.New[sourceKey]()
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

		// We shouldn't blindly accept the namespace value provided by the object.
		// If the object is cluster-scoped, but includes a namespace for some reason,
		// we need to make sure to create the source key with namespace set to
		// corev1.NamespaceAll to ensure that the informer we start actually ends up
		// watch the cluster-scoped object with a cluster-scoped informer.
		mapping, err := c.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return nil, fmt.Errorf("adding %q with GVK %q to set; rest mapping failed: %w", obj.GetName(), gvk, err)
		}

		ns := corev1.NamespaceAll
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			ns = obj.GetNamespace()
		}

		sourceKeys.Insert(sourceKey{ns, gvk})
	}

	return sourceKeys, nil
}
