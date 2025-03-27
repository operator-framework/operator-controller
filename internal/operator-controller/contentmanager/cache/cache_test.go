package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type mockWatcher struct {
	err error
}

var _ Watcher = (*mockWatcher)(nil)

func (mw *mockWatcher) Watch(source.Source) error {
	return mw.err
}

type mockRESTMapper struct {
	mappings map[schema.GroupVersionKind]*meta.RESTMapping
}

var _ meta.RESTMapper = (*mockRESTMapper)(nil)

func (m *mockRESTMapper) KindFor(_ schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	panic("unused")
}

func (m *mockRESTMapper) KindsFor(_ schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	panic("unused")
}

func (m *mockRESTMapper) ResourceFor(_ schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	panic("unused")
}

func (m *mockRESTMapper) ResourcesFor(_ schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	panic("unused")
}

func (m *mockRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	if len(versions) != 1 {
		panic("always expect 1 version for mock rest mapping")
	}
	mapping, ok := m.mappings[gk.WithVersion(versions[0])]
	if !ok {
		return nil, &meta.NoKindMatchError{
			GroupKind:        gk,
			SearchedVersions: versions,
		}
	}
	return mapping, nil
}

func (m *mockRESTMapper) RESTMappings(_ schema.GroupKind, _ ...string) ([]*meta.RESTMapping, error) {
	panic("unused")
}

func (m *mockRESTMapper) ResourceSingularizer(_ string) (string, error) {
	panic("unused")
}

var testRESTMapper = &mockRESTMapper{
	mappings: map[schema.GroupVersionKind]*meta.RESTMapping{
		corev1.SchemeGroupVersion.WithKind("Pod"): {
			Resource:         corev1.SchemeGroupVersion.WithResource("pods"),
			GroupVersionKind: corev1.SchemeGroupVersion.WithKind("Pod"),
			Scope:            meta.RESTScopeNamespace,
		},
		corev1.SchemeGroupVersion.WithKind("Secret"): {
			Resource:         corev1.SchemeGroupVersion.WithResource("secrets"),
			GroupVersionKind: corev1.SchemeGroupVersion.WithKind("Secret"),
			Scope:            meta.RESTScopeNamespace,
		},
		corev1.SchemeGroupVersion.WithKind("Namespace"): {
			Resource:         corev1.SchemeGroupVersion.WithResource("namespaces"),
			GroupVersionKind: corev1.SchemeGroupVersion.WithKind("Namespace"),
			Scope:            meta.RESTScopeRoot,
		},
	},
}

type mockSourcerer struct {
	err    error
	source CloserSyncingSource
}

var _ sourcerer = (*mockSourcerer)(nil)

func (ms *mockSourcerer) Source(_ string, _ schema.GroupVersionKind, _ client.Object, _ func(context.Context)) (CloserSyncingSource, error) {
	if ms.err != nil {
		return nil, ms.err
	}
	return ms.source, nil
}

type mockSource struct {
	err error
}

var _ CloserSyncingSource = (*mockSource)(nil)

func (ms *mockSource) Start(_ context.Context, _ workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	return ms.err
}

func (ms *mockSource) WaitForSync(ctx context.Context) error {
	return ms.err
}

func (ms *mockSource) Close() error {
	return ms.err
}

func TestCacheWatch(t *testing.T) {
	c := NewCache(
		&mockSourcerer{
			source: &mockSource{},
		},
		&ocv1.ClusterExtension{},
		time.Second,
		testRESTMapper,
	)

	pod := &corev1.Pod{}
	pod.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))
	pod.SetNamespace(rand.String(8))

	require.NoError(t, c.Watch(context.Background(), &mockWatcher{}, pod))
	require.Contains(t, c.(*cache).sources, sourceKey{pod.Namespace, pod.GroupVersionKind()}, "sources", c.(*cache).sources)
}

func TestCacheWatchClusterScopedIgnoresNamespace(t *testing.T) {
	c := NewCache(
		&mockSourcerer{
			source: &mockSource{},
		},
		&ocv1.ClusterExtension{},
		time.Second,
		testRESTMapper,
	)

	ns := &corev1.Namespace{}
	ns.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Namespace"))
	ns.SetNamespace(rand.String(8))

	require.NoError(t, c.Watch(context.Background(), &mockWatcher{}, ns))
	require.Contains(t, c.(*cache).sources, sourceKey{corev1.NamespaceAll, ns.GroupVersionKind()}, "sources", c.(*cache).sources)
}

func TestCacheWatchInvalidGVK(t *testing.T) {
	c := NewCache(
		&mockSourcerer{
			source: &mockSource{},
		},
		&ocv1.ClusterExtension{},
		time.Second,
		testRESTMapper,
	)

	pod := &corev1.Pod{}
	require.Error(t, c.Watch(context.Background(), &mockWatcher{}, pod), "should fail on invalid GVK")
}

func TestCacheWatchSourcererError(t *testing.T) {
	c := NewCache(
		&mockSourcerer{
			err: errors.New("error"),
		},
		&ocv1.ClusterExtension{},
		time.Second,
		testRESTMapper,
	)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)
	require.Error(t, c.Watch(context.Background(), &mockWatcher{}, pod), "should fail when sourcerer returns an error")
}

func TestCacheWatchWatcherError(t *testing.T) {
	c := NewCache(
		&mockSourcerer{
			source: &mockSource{},
		},
		&ocv1.ClusterExtension{},
		time.Second,
		testRESTMapper,
	)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)
	require.Error(t, c.Watch(context.Background(), &mockWatcher{err: errors.New("error")}, pod), "should fail when watcher returns an error")
}

func TestCacheWatchSourceWaitForSyncError(t *testing.T) {
	c := NewCache(
		&mockSourcerer{
			source: &mockSource{
				err: errors.New("error"),
			},
		},
		&ocv1.ClusterExtension{},
		time.Second,
		testRESTMapper,
	)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)
	require.Error(t, c.Watch(context.Background(), &mockWatcher{}, pod), "should fail when source fails to sync")
	require.NotContains(t, c.(*cache).sources, podGvk, "should not contain source entry in mapping")
}

func TestCacheWatchExistingSourceNotPanic(t *testing.T) {
	c := NewCache(
		&mockSourcerer{
			source: &mockSource{},
		},
		&ocv1.ClusterExtension{},
		time.Second,
		testRESTMapper,
	)

	pod := &corev1.Pod{}
	pod.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))
	pod.SetNamespace(rand.String(8))
	require.NoError(t, c.(*cache).addSource(sourceKey{pod.Namespace, pod.GroupVersionKind()}, &mockSource{}))

	// In this case, a panic means there is a logic error somewhere in the
	// cache.Watch() method. It should never hit the condition where it panics
	// as it should never attempt to create a new source for one that already exists.
	require.NotPanics(t, func() { _ = c.Watch(context.Background(), &mockWatcher{}, pod) }, "should never panic")
}

func TestCacheWatchRemovesStaleSources(t *testing.T) {
	c := NewCache(
		&mockSourcerer{
			source: &mockSource{},
		},
		&ocv1.ClusterExtension{},
		time.Second,
		testRESTMapper,
	)

	pod := &corev1.Pod{}
	pod.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))
	pod.SetNamespace(rand.String(8))

	require.NoError(t, c.Watch(context.Background(), &mockWatcher{}, pod))
	require.Contains(t, c.(*cache).sources, sourceKey{pod.Namespace, pod.GroupVersionKind()})

	secret := &corev1.Secret{}
	secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	secret.SetNamespace(rand.String(8))
	require.NoError(t, c.Watch(context.Background(), &mockWatcher{}, secret))
	require.Contains(t, c.(*cache).sources, sourceKey{secret.Namespace, secret.GroupVersionKind()})
	require.NotContains(t, c.(*cache).sources, sourceKey{pod.Namespace, pod.GroupVersionKind()})
}

func TestCacheWatchRemovingStaleSourcesError(t *testing.T) {
	c := NewCache(
		&mockSourcerer{
			source: &mockSource{},
		},
		&ocv1.ClusterExtension{},
		time.Second,
		testRESTMapper,
	)

	podSourceKey := sourceKey{
		namespace: rand.String(8),
		gvk:       corev1.SchemeGroupVersion.WithKind("Pod"),
	}
	require.NoError(t, c.(*cache).addSource(podSourceKey, &mockSource{
		err: errors.New("error"),
	}))

	secret := &corev1.Secret{}
	secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	secret.SetNamespace(rand.String(8))
	require.Error(t, c.Watch(context.Background(), &mockWatcher{}, secret))
}
