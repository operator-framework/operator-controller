package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	ocv1 "github.com/operator-framework/operator-controller/api/operator-controller/v1"
)

type mockWatcher struct {
	err error
}

var _ Watcher = (*mockWatcher)(nil)

func (mw *mockWatcher) Watch(source.Source) error {
	return mw.err
}

type mockSourcerer struct {
	err    error
	source CloserSyncingSource
}

var _ sourcerer = (*mockSourcerer)(nil)

func (ms *mockSourcerer) Source(_ schema.GroupVersionKind, _ client.Object, _ func(context.Context)) (CloserSyncingSource, error) {
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
	)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)

	require.NoError(t, c.Watch(context.Background(), &mockWatcher{}, pod))
	require.Contains(t, c.(*cache).sources, podGvk, "sources", c.(*cache).sources)
}

func TestCacheWatchInvalidGVK(t *testing.T) {
	c := NewCache(
		&mockSourcerer{
			source: &mockSource{},
		},
		&ocv1.ClusterExtension{},
		time.Second,
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
	)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)
	require.NoError(t, c.(*cache).addSource(podGvk, &mockSource{}))

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
	)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)

	require.NoError(t, c.Watch(context.Background(), &mockWatcher{}, pod))
	require.Contains(t, c.(*cache).sources, podGvk)

	secret := &corev1.Secret{}
	secretGvk := corev1.SchemeGroupVersion.WithKind("Secret")
	secret.SetGroupVersionKind(secretGvk)
	require.NoError(t, c.Watch(context.Background(), &mockWatcher{}, secret))
	require.Contains(t, c.(*cache).sources, secretGvk)
	require.NotContains(t, c.(*cache).sources, podGvk)
}

func TestCacheWatchRemovingStaleSourcesError(t *testing.T) {
	c := NewCache(
		&mockSourcerer{
			source: &mockSource{},
		},
		&ocv1.ClusterExtension{},
		time.Second,
	)

	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	require.NoError(t, c.(*cache).addSource(podGvk, &mockSource{
		err: errors.New("error"),
	}))

	secret := &corev1.Secret{}
	secretGvk := corev1.SchemeGroupVersion.WithKind("Secret")
	secret.SetGroupVersionKind(secretGvk)
	require.Error(t, c.Watch(context.Background(), &mockWatcher{}, secret))
}
