package catalogsource

import (
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/json"
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

func WithRegistryClient(r RegistryClient) Option {
	return func(c *CachedRegistryEntitySource) {
		c.rClient = r
	}
}

func WithLogger(l logr.Logger) Option {
	return func(c *CachedRegistryEntitySource) {
		c.logger = &l
	}
}

func NewCachedRegistryQuerier(client client.WithWatch, options ...Option) *CachedRegistryEntitySource {
	l := zap.New()
	logger := &l
	c := &CachedRegistryEntitySource{
		client:       client,
		rClient:      NewRegistryGRPCClient(0),
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
	go func() {
		for {
			item, shutdown := r.queue.Get() // block till there is a new item
			if shutdown {
				return
			}
			if key, ok := item.(types.NamespacedName); ok {
				r.syncCatalogSource(ctx, key)
			}
		}
	}()

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

			r.queue.Add(catalogSourceKey(&catalogSource))
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

// handle added or updated catalogSource
func (r *CachedRegistryEntitySource) syncCatalogSource(ctx context.Context, key types.NamespacedName) {
	r.RWMutex.Lock()
	defer r.queue.Done(key)
	defer r.RWMutex.Unlock()

	var catalogSource v1alpha1.CatalogSource
	if err := r.client.Get(ctx, key, &catalogSource); err != nil {
		if errors2.IsNotFound(err) {
			delete(r.cache, catalogSourceKey(&catalogSource).String())
			return
		} else {
			r.logger.Info("cannot find catalogSource, skipping cache update", "CatalogSource", key)
			r.queue.AddRateLimited(key)
			return
		}
	}

	entities, err := r.rClient.ListEntities(ctx, &catalogSource)
	if err != nil {
		r.logger.Error(err, "failed to list entities for catalogSource entity cache update", "catalogSource", key)
		if !isManagedCatalogSource(catalogSource) {
			r.queue.AddRateLimited(key)
		}
		return
	}
	r.cache[key.String()] = sourceCache{
		Items: entities,
	}
	if !isManagedCatalogSource(catalogSource) {
		r.queue.Forget(key)
		r.queue.AddAfter(key, r.syncInterval)
	}
	r.logger.Info("Completed cache update", "catalogSource", key)
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
		r.cache[catalogSourceKey(&catalogSource).String()] = sourceCache{
			Items: entities,
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

func catalogSourceKey(source *v1alpha1.CatalogSource) *types.NamespacedName {
	if source == nil {
		return nil
	}
	return &types.NamespacedName{Namespace: source.Namespace, Name: source.Name}
}
