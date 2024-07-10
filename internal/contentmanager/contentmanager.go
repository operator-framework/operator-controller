package contentmanager

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/operator-framework/operator-controller/api/v1alpha1"
	oclabels "github.com/operator-framework/operator-controller/internal/labels"
)

type Watcher interface {
	// Watch will establish watches for resources owned by a ClusterExtension
	Watch(context.Context, controller.Controller, *v1alpha1.ClusterExtension, []client.Object) error
	// Unwatch will remove watches for a ClusterExtension
	Unwatch(*v1alpha1.ClusterExtension)
}

type RestConfigMapper func(context.Context, client.Object, *rest.Config) (*rest.Config, error)

type extensionCacheData struct {
	Cache  cache.Cache
	Cancel context.CancelFunc
}

type instance struct {
	rcm             RestConfigMapper
	baseCfg         *rest.Config
	extensionCaches map[string]extensionCacheData
	mapper          meta.RESTMapper
	mu              *sync.Mutex
}

// New creates a new ContentManager object
func New(rcm RestConfigMapper, cfg *rest.Config, mapper meta.RESTMapper) Watcher {
	return &instance{
		rcm:             rcm,
		baseCfg:         cfg,
		extensionCaches: make(map[string]extensionCacheData),
		mapper:          mapper,
		mu:              &sync.Mutex{},
	}
}

// buildScheme builds a runtime.Scheme based on the provided client.Objects,
// with all GroupVersionKinds mapping to the unstructured.Unstructured type
// (unstructured.UnstructuredList for list kinds).
//
// If a provided client.Object does not set a Version or Kind field in its
// GroupVersionKind, an error will be returned.
func buildScheme(objs []client.Object) (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	// The ClusterExtension types must be added to the scheme since its
	// going to be used to establish watches that trigger reconciliation
	// of the owning ClusterExtension
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("adding operator controller APIs to scheme: %w", err)
	}

	for _, obj := range objs {
		gvk := obj.GetObjectKind().GroupVersionKind()

		// If the Kind or Version is not set in an object's GroupVersionKind
		// attempting to add it to the runtime.Scheme will result in a panic.
		// To avoid panics, we are doing the validation and returning early
		// with an error if any objects are provided with a missing Kind or Version
		// field
		if gvk.Kind == "" {
			return nil, fmt.Errorf(
				"adding %s to scheme; object Kind is not defined",
				obj.GetName(),
			)
		}

		if gvk.Version == "" {
			return nil, fmt.Errorf(
				"adding %s to scheme; object Version is not defined",
				obj.GetName(),
			)
		}

		listKind := gvk.Kind + "List"

		if !scheme.Recognizes(gvk) {
			// Since we can't have a mapping to every possible Go type in existence
			// based on the GVK we need to use the unstructured types for mapping
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(gvk)
			ul := &unstructured.UnstructuredList{}
			ul.SetGroupVersionKind(gvk.GroupVersion().WithKind(listKind))

			scheme.AddKnownTypeWithName(gvk, u)
			scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(listKind), ul)
			// Adding the common meta schemas to the scheme for the GroupVersion
			// is necessary to ensure the scheme is aware of the different operations
			// that can be performed against the resources in this GroupVersion
			metav1.AddToGroupVersion(scheme, gvk.GroupVersion())
		}
	}

	return scheme, nil
}

// Watch configures a controller-runtime cache.Cache and establishes watches for the provided resources.
// It utilizes the provided ClusterExtension to set a DefaultLabelSelector on the cache.Cache
// to ensure it is only caching and reacting to content that belongs to the ClusterExtension.
// For each client.Object provided, a new source.Kind is created and used in a call to the Watch() method
// of the provided controller.Controller to establish new watches for the managed resources.
func (i *instance) Watch(ctx context.Context, ctrl controller.Controller, ce *v1alpha1.ClusterExtension, objs []client.Object) error {
	if len(objs) == 0 || ce == nil || ctrl == nil {
		return nil
	}

	cfg, err := i.rcm(ctx, ce, i.baseCfg)
	if err != nil {
		return fmt.Errorf("getting rest.Config for ClusterExtension %q: %w", ce.Name, err)
	}

	scheme, err := buildScheme(objs)
	if err != nil {
		return fmt.Errorf("building scheme for ClusterExtension %q: %w", ce.GetName(), err)
	}

	tgtLabels := labels.Set{
		oclabels.OwnerKindKey: v1alpha1.ClusterExtensionKind,
		oclabels.OwnerNameKey: ce.GetName(),
	}

	c, err := cache.New(cfg, cache.Options{
		Scheme:               scheme,
		DefaultLabelSelector: tgtLabels.AsSelector(),
	})
	if err != nil {
		return fmt.Errorf("creating cache for ClusterExtension %q: %w", ce.Name, err)
	}

	for _, obj := range objs {
		err = ctrl.Watch(
			source.Kind(
				c,
				obj,
				handler.TypedEnqueueRequestForOwner[client.Object](
					scheme,
					i.mapper,
					ce,
				),
			),
		)
		if err != nil {
			return fmt.Errorf("creating watch for ClusterExtension %q managed resource %s: %w", ce.Name, obj.GetObjectKind().GroupVersionKind(), err)
		}
	}

	// TODO: Instead of stopping the existing cache and replacing it every time
	// we should stop the informers that are no longer required
	// and create any new ones as necessary. To keep the initial pass
	// simple, we are going to keep this as is and optimize in a follow-up.
	// Doing this in a follow-up gives us the opportunity to verify that this functions
	// as expected when wired up in the ClusterExtension reconciler before going too deep
	// in optimizations.
	i.mu.Lock()
	if extCache, ok := i.extensionCaches[ce.GetName()]; ok {
		extCache.Cancel()
	}

	cacheCtx, cancel := context.WithCancel(context.Background())
	i.extensionCaches[ce.Name] = extensionCacheData{
		Cache:  c,
		Cancel: cancel,
	}
	i.mu.Unlock()

	go func() {
		err := c.Start(cacheCtx)
		if err != nil {
			i.Unwatch(ce)
		}
	}()

	if !c.WaitForCacheSync(cacheCtx) {
		i.Unwatch(ce)
		return errors.New("cache could not sync, it has been stopped and removed")
	}

	return nil
}

// Unwatch will stop the cache for the provided ClusterExtension
// stopping any watches on managed content
func (i *instance) Unwatch(ce *v1alpha1.ClusterExtension) {
	if ce == nil {
		return
	}

	i.mu.Lock()
	if extCache, ok := i.extensionCaches[ce.GetName()]; ok {
		extCache.Cancel()
		delete(i.extensionCaches, ce.GetName())
	}
	i.mu.Unlock()
}
