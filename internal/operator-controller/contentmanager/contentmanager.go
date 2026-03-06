package contentmanager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	cmcache "github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager/cache"
	oclabels "github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

// Manager watches managed content objects across all ClusterExtensions
// using a single set of shared informers with a broad label selector.
type Manager interface {
	io.Closer
	// Watch ensures informers are running for the GVKs of all provided objects,
	// tracked under the given ClusterExtension name. GVKs no longer needed by
	// this CE (present in a previous Watch call but absent now) are released,
	// and their informers are stopped if no other CE still needs them.
	// Informers use a label selector matching all ClusterExtension-owned objects,
	// and event routing to the correct CE is handled by owner references.
	Watch(ctx context.Context, ceName string, watcher cmcache.Watcher, objs ...client.Object) error
	// Delete removes all GVK tracking for the given ClusterExtension and stops
	// any informers that are no longer needed by any remaining CE.
	Delete(ctx context.Context, ceName string)
}

type managerImpl struct {
	sourcerer    sourcerer
	sources      map[schema.GroupVersionKind]cmcache.CloserSyncingSource
	ceGVKs       map[string]sets.Set[schema.GroupVersionKind] // per-CE GVK tracking
	mu           sync.Mutex
	syncTimeout  time.Duration
	resyncPeriod time.Duration
}

type sourcerer interface {
	Source(schema.GroupVersionKind, client.Object, func(context.Context)) (cmcache.CloserSyncingSource, error)
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

// NewManager creates a new Manager that uses a single dynamic client and
// shared informers to watch all ClusterExtension-managed objects.
func NewManager(cfg *rest.Config, mapper meta.RESTMapper, opts ...ManagerOption) (Manager, error) {
	m := &managerImpl{
		sources:      make(map[schema.GroupVersionKind]cmcache.CloserSyncingSource),
		ceGVKs:       make(map[string]sets.Set[schema.GroupVersionKind]),
		syncTimeout:  time.Second * 10,
		resyncPeriod: time.Hour * 10,
	}

	for _, opt := range opts {
		opt(m)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}

	tgtLabels := labels.Set{
		oclabels.OwnerKindKey: ocv1.ClusterExtensionKind,
	}

	m.sourcerer = &dynamicSourcerer{
		informerFactoryCreateFunc: func() dynamicinformer.DynamicSharedInformerFactory {
			return dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, m.resyncPeriod, metav1.NamespaceAll, func(lo *metav1.ListOptions) {
				lo.LabelSelector = tgtLabels.AsSelector().String()
			})
		},
		mapper: mapper,
	}

	return m, nil
}

// Watch ensures informers are running for the GVKs of all provided objects.
// It updates the GVK set for the named CE and stops informers for GVKs that
// are no longer needed by any CE.
func (m *managerImpl) Watch(ctx context.Context, ceName string, watcher cmcache.Watcher, objs ...client.Object) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newGVKs := sets.New[schema.GroupVersionKind]()
	for _, obj := range objs {
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Kind == "" {
			return fmt.Errorf("object %s has no Kind set", obj.GetName())
		}
		if gvk.Version == "" {
			return fmt.Errorf("object %s has no Version set", obj.GetName())
		}
		newGVKs.Insert(gvk)
	}

	oldGVKs := m.ceGVKs[ceName]
	m.ceGVKs[ceName] = newGVKs

	// Start informers for GVKs not yet watched
	for gvk := range newGVKs {
		if _, exists := m.sources[gvk]; exists {
			continue
		}
		s, err := m.startSource(ctx, gvk, watcher)
		if err != nil {
			return fmt.Errorf("starting source for GVK %q: %w", gvk, err)
		}
		m.sources[gvk] = s
	}

	// Stop informers for GVKs this CE no longer needs, if no other CE needs them
	if oldGVKs != nil {
		removed := oldGVKs.Difference(newGVKs)
		m.stopOrphanedSources(ctx, removed)
	}

	return nil
}

// Delete removes all GVK tracking for the named CE and stops orphaned informers.
func (m *managerImpl) Delete(ctx context.Context, ceName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	gvks, ok := m.ceGVKs[ceName]
	if !ok {
		return
	}
	delete(m.ceGVKs, ceName)
	m.stopOrphanedSources(ctx, gvks)
}

// stopOrphanedSources stops informers for GVKs that no CE still requires.
// Must be called with m.mu held.
func (m *managerImpl) stopOrphanedSources(ctx context.Context, candidates sets.Set[schema.GroupVersionKind]) {
	for gvk := range candidates {
		if m.isGVKNeeded(gvk) {
			continue
		}
		if source, ok := m.sources[gvk]; ok {
			if err := source.Close(); err != nil {
				logr := log.FromContext(ctx)
				logr.Error(err, "closing orphaned source failed", "gvk", gvk)
			}
			delete(m.sources, gvk)
		}
	}
}

// isGVKNeeded returns true if any CE still requires this GVK.
// Must be called with m.mu held.
func (m *managerImpl) isGVKNeeded(gvk schema.GroupVersionKind) bool {
	for _, gvks := range m.ceGVKs {
		if gvks.Has(gvk) {
			return true
		}
	}
	return false
}

func (m *managerImpl) startSource(ctx context.Context, gvk schema.GroupVersionKind, watcher cmcache.Watcher) (cmcache.CloserSyncingSource, error) {
	// Use a placeholder ClusterExtension as the owner type for EnqueueRequestForOwner.
	// The handler uses the owner's GVK to match ownerReferences, not the specific instance.
	owner := &ocv1.ClusterExtension{}

	s, err := m.sourcerer.Source(gvk, owner, func(ctx context.Context) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if source, ok := m.sources[gvk]; ok {
			if closeErr := source.Close(); closeErr != nil {
				logr := log.FromContext(ctx)
				logr.Error(closeErr, "managed content cache postSyncError removing source failed", "gvk", gvk)
			}
			delete(m.sources, gvk)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("getting source: %w", err)
	}

	if err := watcher.Watch(s); err != nil {
		return nil, fmt.Errorf("establishing watch for GVK %q: %w", gvk, err)
	}

	syncCtx, syncCancel := context.WithTimeout(ctx, m.syncTimeout)
	defer syncCancel()
	if err := s.WaitForSync(syncCtx); err != nil {
		return nil, fmt.Errorf("waiting for sync: %w", err)
	}

	return s, nil
}

// Close stops all informers managed by this Manager.
func (m *managerImpl) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for gvk, s := range m.sources {
		if err := s.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing source for GVK %q: %w", gvk, err))
		}
	}
	clear(m.sources)
	clear(m.ceGVKs)
	return errors.Join(errs...)
}
