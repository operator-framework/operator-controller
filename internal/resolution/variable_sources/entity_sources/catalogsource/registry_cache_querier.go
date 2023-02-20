package catalogsource

import (
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

const defaultCatalogSourceSyncInterval = 5 * time.Minute

type CachedRegistryEntitySource struct {
	sync.RWMutex
	client       client.WithWatch
	rClient      RegistryClient
	logger       *logr.Logger
	cache        map[string]sourceCache
	done         chan struct{}
	queue        workqueue.RateLimitingInterface // for unmanaged catalogsources
	syncInterval time.Duration
}

type sourceCache struct {
	Items []*input.Entity
}

type Option func(*CachedRegistryEntitySource)

func WithSyncInterval(d time.Duration) Option {
	return func(c *CachedRegistryEntitySource) {
		c.syncInterval = d
	}
}

func NewCachedRegistryQuerier(client client.WithWatch, rClient RegistryClient, logger *logr.Logger, options ...Option) *CachedRegistryEntitySource {
	if logger == nil {
		l := zap.New()
		logger = &l
	}
	c := &CachedRegistryEntitySource{
		client:       client,
		rClient:      rClient,
		logger:       logger,
		done:         make(chan struct{}),
		cache:        map[string]sourceCache{},
		queue:        workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		syncInterval: defaultCatalogSourceSyncInterval,
	}
	for _, o := range options {
		o(c)
	}
	return c
}

func (r *CachedRegistryEntitySource) Start(ctx context.Context) error {
	// TODO: constraints for limiting watched catalogSources
	// TODO: respect CatalogSource priorities
	catalogSourceWatch, err := r.client.Watch(ctx, &v1alpha1.CatalogSourceList{})
	if err != nil {
		r.logger.Error(err, "failed to start catalogsource watch")
		return err
	}
	r.logger.Info("starting CatalogSource cache")
	if err := r.populate(ctx); err != nil {
		r.logger.Error(err, "error populating initial entity cache")
	} else {
		r.logger.Info("Populated initial cache")
	}
	// watching catalogSource for changes works only with OLM managed catalogSources.
	go r.ProcessQueue(ctx)
	for {
		select {
		case entry := <-catalogSourceWatch.ResultChan():
			encodedObj, err := json.Marshal(entry.Object)
			if err != nil {
				r.logger.Error(err, "cannot reconcile non-catalogSource: marshalling failed", "event", entry.Type, "object", entry.Object)
				continue
			}
			catalogSource := v1alpha1.CatalogSource{}
			err = json.Unmarshal(encodedObj, &catalogSource)
			if err != nil {
				r.logger.Error(err, "cannot reconcile non-catalogSource: unmarshalling failed", "event", entry.Type, "object", entry.Object)
				continue
			}

			switch entry.Type {
			case watch.Deleted:
				func() {
					r.RWMutex.Lock()
					defer r.RWMutex.Unlock()
					catalogSourceKey := types.NamespacedName{Namespace: catalogSource.Namespace, Name: catalogSource.Name}
					delete(r.cache, catalogSourceKey.String())
					r.logger.Info("Completed cache delete", "catalogSource", catalogSourceKey)
				}()
			case watch.Added, watch.Modified:
				r.syncCatalogSource(ctx, catalogSource)
			}
		case <-ctx.Done():
			r.Stop()
		case <-r.done:
			// wait till shutdown completes
			return ctx.Err()
		}
	}
}

func (r *CachedRegistryEntitySource) Stop() {
	r.RWMutex.Lock()
	defer r.RWMutex.Unlock()
	r.logger.Info("stopping CatalogSource cache")
	r.queue.ShutDown()
	close(r.done)
}

func (r *CachedRegistryEntitySource) ProcessQueue(ctx context.Context) {
	for {
		item, _ := r.queue.Get() // block till there is a new item
		defer r.queue.Done(item)
		if _, ok := item.(types.NamespacedName); ok {
			var catalogSource v1alpha1.CatalogSource
			if err := r.client.Get(ctx, item.(types.NamespacedName), &catalogSource); err != nil {
				r.logger.Info("cannot find catalogSource, skipping cache update", "CatalogSource", item)
				return
			}
			r.syncCatalogSource(ctx, catalogSource)
		}
	}
}

// handle added or updated catalogSource
func (r *CachedRegistryEntitySource) syncCatalogSource(ctx context.Context, catalogSource v1alpha1.CatalogSource) {
	catalogSourceKey := types.NamespacedName{Namespace: catalogSource.Namespace, Name: catalogSource.Name}
	r.RWMutex.Lock()
	defer r.RWMutex.Unlock()
	entities, err := r.rClient.ListEntities(ctx, &catalogSource)
	if err != nil {
		r.logger.Error(err, "failed to list entities for catalogSource entity cache update", "catalogSource", catalogSourceKey)
		if !isManagedCatalogSource(catalogSource) {
			r.queue.AddRateLimited(catalogSourceKey)
		}
		return
	}
	r.cache[catalogSourceKey.String()] = sourceCache{
		Items: entities,
		//imageID: imageID,
	}
	if !isManagedCatalogSource(catalogSource) {
		r.queue.Forget(catalogSourceKey)
		r.queue.AddAfter(catalogSourceKey, r.syncInterval)
	}
	r.logger.Info("Completed cache update", "catalogSource", catalogSourceKey)
}

func (r *CachedRegistryEntitySource) Get(ctx context.Context, id deppy.Identifier) *input.Entity {
	r.RWMutex.RLock()
	defer r.RWMutex.RUnlock()
	for _, entries := range r.cache {
		for _, entity := range entries.Items {
			if entity.Identifier() == id {
				return entity
			}
		}
	}
	return nil
}

func (r *CachedRegistryEntitySource) Filter(ctx context.Context, filter input.Predicate) (input.EntityList, error) {
	r.RWMutex.RLock()
	defer r.RWMutex.RUnlock()
	resultSet := input.EntityList{}
	for _, entries := range r.cache {
		for _, entity := range entries.Items {
			if filter(entity) {
				resultSet = append(resultSet, *entity)
			}
		}
	}
	return resultSet, nil
}

func (r *CachedRegistryEntitySource) GroupBy(ctx context.Context, fn input.GroupByFunction) (input.EntityListMap, error) {
	r.RWMutex.RLock()
	defer r.RWMutex.RUnlock()
	resultSet := input.EntityListMap{}
	for _, entries := range r.cache {
		for _, entity := range entries.Items {
			keys := fn(entity)
			for _, key := range keys {
				resultSet[key] = append(resultSet[key], *entity)
			}
		}
	}
	return resultSet, nil
}

func (r *CachedRegistryEntitySource) Iterate(ctx context.Context, fn input.IteratorFunction) error {
	r.RWMutex.RLock()
	defer r.RWMutex.RUnlock()
	for _, entries := range r.cache {
		for _, entity := range entries.Items {
			if err := fn(entity); err != nil {
				return err
			}
		}
	}
	return nil
}

// populate initializes the state of an empty cache from the catalogSources currently present on the cluster
func (r *CachedRegistryEntitySource) populate(ctx context.Context) error {
	r.RWMutex.Lock()
	defer r.RWMutex.Unlock()
	catalogSourceList := v1alpha1.CatalogSourceList{}
	if err := r.client.List(ctx, &catalogSourceList); err != nil {
		return err
	}
	var errs []error
	for _, catalogSource := range catalogSourceList.Items {
		entities, err := r.rClient.ListEntities(ctx, &catalogSource)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		catalogSourceKey := types.NamespacedName{Namespace: catalogSource.Namespace, Name: catalogSource.Name}
		r.cache[catalogSourceKey.String()] = sourceCache{
			Items: entities,
			//imageID: imageID,
		}
	}
	if len(errs) > 0 {
		return errors.NewAggregate(errs)
	}
	return nil
}

// TODO: find better way to identify catalogSources unmanaged by olm
func isManagedCatalogSource(catalogSource v1alpha1.CatalogSource) bool {
	return len(catalogSource.Spec.Address) == 0
}
