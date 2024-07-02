package contentmanager

import (
	"context"
	"errors"
	"fmt"

	"github.com/operator-framework/operator-controller/api/v1alpha1"
	oclabels "github.com/operator-framework/operator-controller/internal/labels"
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
)

type ContentManager interface {
	// ManageContent will:
	// 1. Create a new controller-runtime cache.Cache belonging to the provided ClusterExtension
	// 2. For each object provided:
	//   A. Use the provided controller.Controller to establish a watch for the resource
	ManageContent(context.Context, controller.Controller, *v1alpha1.ClusterExtension, []client.Object) error
	// RemoveManagedContent will:
	// 1. Remove/stop cache and any sources/informers for the provided ClusterExtension
	RemoveManagedContent(*v1alpha1.ClusterExtension) error
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
}

func New(rcm RestConfigMapper, cfg *rest.Config, mapper meta.RESTMapper) ContentManager {
	return &instance{
		rcm:             rcm,
		baseCfg:         cfg,
		extensionCaches: make(map[string]extensionCacheData),
		mapper:          mapper,
	}
}

func (i *instance) ManageContent(ctx context.Context, ctrl controller.Controller, ce *v1alpha1.ClusterExtension, objs []client.Object) error {
	cfg, err := i.rcm(ctx, ce, i.baseCfg)
	if err != nil {
		return fmt.Errorf("getting rest.Config for ClusterExtension %q: %w", ce.Name, err)
	}

	// TODO: add a http.RoundTripper to the config to ensure it is always using an up
	// to date authentication token for the ServiceAccount token provided in the ClusterExtension.
	// Maybe this should be handled by the RestConfigMapper

	// Assumptions: all objects received by the function will have the Object metadata specfically,
	// ApiVersion and Kind set. Failure to which the code will panic when adding the types to the scheme
	scheme := runtime.NewScheme()
	for _, obj := range objs {
		gvk := obj.GetObjectKind().GroupVersionKind()
		listKind := obj.GetObjectKind().GroupVersionKind().Kind + "List"

		if gvk.Kind == "" || gvk.Version == "" {
			return errors.New("object Kind or Version is not defined")
		}

		if !scheme.Recognizes(gvk) {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(gvk)
			ul := &unstructured.UnstructuredList{}

			ul.SetGroupVersionKind(gvk.GroupVersion().WithKind(listKind))

			scheme.AddKnownTypeWithName(gvk, u)
			scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(listKind), ul)
			metav1.AddToGroupVersion(scheme, gvk.GroupVersion())
		}
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
				nil,
			),
		)
		if err != nil {
			return fmt.Errorf("creating watch for ClusterExtension %q managed resource %s: %w", ce.Name, obj.GetObjectKind().GroupVersionKind(), err)
		}
	}

	if data, ok := i.extensionCaches[ce.GetName()]; ok {
		data.Cancel()
	}

	ctx, cancel := context.WithCancel(ctx)
	go c.Start(ctx)
	i.extensionCaches[ce.Name] = extensionCacheData{
		Cache:  c,
		Cancel: cancel,
	}

	return nil
}

func (i *instance) RemoveManagedContent(ce *v1alpha1.ClusterExtension) error {
	if data, ok := i.extensionCaches[ce.GetName()]; ok {
		data.Cancel()
		delete(i.extensionCaches, ce.GetName())
	}

	return nil
}
