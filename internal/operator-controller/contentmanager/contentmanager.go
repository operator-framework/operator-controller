package contentmanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/operator-framework/operator-controller/api/v1"
	cmcache "github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager/cache"
	oclabels "github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

// Manager is a utility to manage content caches belonging
// to ClusterExtensions
type Manager interface {
	// Get returns a managed content cache for the provided
	// ClusterExtension if one exists. If one does not exist,
	// a new Cache is created and returned
	Get(context.Context, *v1.ClusterExtension) (cmcache.Cache, error)
	// Delete will stop and remove a managed content cache
	// for the provided ClusterExtension if one exists.
	Delete(*v1.ClusterExtension) error
}

type RestConfigMapper func(context.Context, client.Object, *rest.Config) (*rest.Config, error)

// managerImpl is an implementation of the Manager interface
type managerImpl struct {
	rcm          RestConfigMapper
	baseCfg      *rest.Config
	caches       map[string]cmcache.Cache
	mapper       meta.RESTMapper
	mu           *sync.Mutex
	syncTimeout  time.Duration
	resyncPeriod time.Duration
}

type ManagerOption func(*managerImpl)

// WithSyncTimeout configures the time spent waiting
// for a managed content source to sync
func WithSyncTimeout(t time.Duration) ManagerOption {
	return func(m *managerImpl) {
		m.syncTimeout = t
	}
}

// WithResyncPeriod configures the frequency
// a managed content source attempts to resync
func WithResyncPeriod(t time.Duration) ManagerOption {
	return func(m *managerImpl) {
		m.resyncPeriod = t
	}
}

// NewManager creates a new Manager
func NewManager(rcm RestConfigMapper, cfg *rest.Config, mapper meta.RESTMapper, opts ...ManagerOption) Manager {
	m := &managerImpl{
		rcm:          rcm,
		baseCfg:      cfg,
		caches:       make(map[string]cmcache.Cache),
		mapper:       mapper,
		mu:           &sync.Mutex{},
		syncTimeout:  time.Second * 10,
		resyncPeriod: time.Hour * 10,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Get returns a Cache for the provided ClusterExtension.
// If a cache does not already exist, a new one will be created.
// If a nil ClusterExtension is provided this function will panic.
func (i *managerImpl) Get(ctx context.Context, ce *v1.ClusterExtension) (cmcache.Cache, error) {
	if ce == nil {
		panic("nil ClusterExtension provided")
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	cache, ok := i.caches[ce.Name]
	if ok {
		return cache, nil
	}

	cfg, err := i.rcm(ctx, ce, i.baseCfg)
	if err != nil {
		return nil, fmt.Errorf("getting rest.Config: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("getting dynamic client: %w", err)
	}

	tgtLabels := labels.Set{
		oclabels.OwnerKindKey: v1.ClusterExtensionKind,
		oclabels.OwnerNameKey: ce.GetName(),
	}

	dynamicSourcerer := &dynamicSourcerer{
		// Due to the limitation outlined in the dynamic informer source,
		// related to reusing an informer factory, we return a new informer
		// factory every time to ensure we are not attempting to configure or
		// start an already started informer
		informerFactoryCreateFunc: func() dynamicinformer.DynamicSharedInformerFactory {
			return dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, time.Hour*10, metav1.NamespaceAll, func(lo *metav1.ListOptions) {
				lo.LabelSelector = tgtLabels.AsSelector().String()
			})
		},
		mapper: i.mapper,
	}
	cache = cmcache.NewCache(dynamicSourcerer, ce, i.syncTimeout)
	i.caches[ce.Name] = cache
	return cache, nil
}

// Delete stops and removes the Cache for the provided ClusterExtension
func (i *managerImpl) Delete(ce *v1.ClusterExtension) error {
	if ce == nil {
		panic("nil ClusterExtension provided")
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	if cache, ok := i.caches[ce.Name]; ok {
		err := cache.Close()
		if err != nil {
			return fmt.Errorf("closing cache: %w", err)
		}
		delete(i.caches, ce.Name)
	}
	return nil
}
