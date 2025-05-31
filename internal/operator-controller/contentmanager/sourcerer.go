package contentmanager

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager/cache"
	"github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager/source"
)

type dynamicSourcerer struct {
	informerFactoryCreateFunc func(namespace string) dynamicinformer.DynamicSharedInformerFactory
	mapper                    meta.RESTMapper
}

func (ds *dynamicSourcerer) Source(namespace string, gvk schema.GroupVersionKind, owner client.Object, onPostSyncError func(context.Context)) (cache.CloserSyncingSource, error) {
	scheme, err := buildScheme(gvk)
	if err != nil {
		return nil, fmt.Errorf("building scheme: %w", err)
	}

	restMapping, err := ds.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("getting resource mapping for GVK %q: %w", gvk, err)
	}

	s := source.NewDynamicSource(source.DynamicSourceConfig{
		GVR:     restMapping.Resource,
		Owner:   owner,
		Handler: handler.EnqueueRequestForOwner(scheme, ds.mapper, owner, handler.OnlyControllerOwner()),
		Predicates: []predicate.Predicate{
			predicate.Funcs{
				CreateFunc:  func(tce event.TypedCreateEvent[client.Object]) bool { return false },
				UpdateFunc:  func(tue event.TypedUpdateEvent[client.Object]) bool { return true },
				DeleteFunc:  func(tde event.TypedDeleteEvent[client.Object]) bool { return true },
				GenericFunc: func(tge event.TypedGenericEvent[client.Object]) bool { return true },
			},
		},
		DynamicInformerFactory: ds.informerFactoryCreateFunc(namespace),
		OnPostSyncError:        onPostSyncError,
	})
	return s, nil
}

// buildScheme builds a runtime.Scheme based on the provided GroupVersionKinds,
// with all GroupVersionKinds mapping to the unstructured.Unstructured type
// (unstructured.UnstructuredList for list kinds).
//
// It is assumed all GroupVersionKinds are valid, which means:
//   - The Kind is set
//   - The Version is set
//
// Invalid GroupVersionKinds will result in a panic.
func buildScheme(gvks ...schema.GroupVersionKind) (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	// The ClusterExtension types must be added to the scheme since its
	// going to be used to establish watches that trigger reconciliation
	// of the owning ClusterExtension
	if err := ocv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("adding operator controller APIs to scheme: %w", err)
	}

	for _, gvk := range gvks {
		if !scheme.Recognizes(gvk) {
			// Since we can't have a mapping to every possible Go type in existence
			// based on the GVK we need to use the unstructured types for mapping
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(gvk)
			scheme.AddKnownTypeWithName(gvk, u)

			// Adding the common meta schemas to the scheme for the GroupVersion
			// is necessary to ensure the scheme is aware of the different operations
			// that can be performed against the resources in this GroupVersion
			metav1.AddToGroupVersion(scheme, gvk.GroupVersion())
		}

		listGVK := gvk
		listGVK.Kind = listGVK.Kind + "List"
		if !scheme.Recognizes(listGVK) {
			ul := &unstructured.UnstructuredList{}
			ul.SetGroupVersionKind(listGVK)
			scheme.AddKnownTypeWithName(listGVK, ul)
		}
	}

	return scheme, nil
}
