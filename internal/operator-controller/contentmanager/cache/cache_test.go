package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestCacheWatch(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockSrc := NewMockCloserSyncingSource(ctrl)
	mockSrc.EXPECT().WaitForSync(gomock.Any()).Return(nil)

	mockSrcr := NewMockSourcerer(ctrl)
	mockSrcr.EXPECT().Source(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockSrc, nil)

	mockW := NewMockWatcher(ctrl)
	mockW.EXPECT().Watch(gomock.Any()).Return(nil)

	c := NewCache(mockSrcr, &ocv1.ClusterExtension{}, time.Second)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)

	require.NoError(t, c.Watch(context.Background(), mockW, pod))
	require.Contains(t, c.(*cache).sources, podGvk, "sources", c.(*cache).sources)
}

func TestCacheWatchInvalidGVK(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockSrcr := NewMockSourcerer(ctrl)
	mockW := NewMockWatcher(ctrl)

	c := NewCache(mockSrcr, &ocv1.ClusterExtension{}, time.Second)

	pod := &corev1.Pod{}
	require.Error(t, c.Watch(context.Background(), mockW, pod), "should fail on invalid GVK")
}

func TestCacheWatchSourcererError(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockSrcr := NewMockSourcerer(ctrl)
	mockSrcr.EXPECT().Source(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("error"))

	mockW := NewMockWatcher(ctrl)

	c := NewCache(mockSrcr, &ocv1.ClusterExtension{}, time.Second)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)
	require.Error(t, c.Watch(context.Background(), mockW, pod), "should fail when sourcerer returns an error")
}

func TestCacheWatchWatcherError(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockSrc := NewMockCloserSyncingSource(ctrl)

	mockSrcr := NewMockSourcerer(ctrl)
	mockSrcr.EXPECT().Source(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockSrc, nil)

	mockW := NewMockWatcher(ctrl)
	mockW.EXPECT().Watch(gomock.Any()).Return(errors.New("error"))

	c := NewCache(mockSrcr, &ocv1.ClusterExtension{}, time.Second)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)
	require.Error(t, c.Watch(context.Background(), mockW, pod), "should fail when watcher returns an error")
}

func TestCacheWatchSourceWaitForSyncError(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockSrc := NewMockCloserSyncingSource(ctrl)
	mockSrc.EXPECT().WaitForSync(gomock.Any()).Return(errors.New("error"))

	mockSrcr := NewMockSourcerer(ctrl)
	mockSrcr.EXPECT().Source(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockSrc, nil)

	mockW := NewMockWatcher(ctrl)
	mockW.EXPECT().Watch(gomock.Any()).Return(nil)

	c := NewCache(mockSrcr, &ocv1.ClusterExtension{}, time.Second)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)
	require.Error(t, c.Watch(context.Background(), mockW, pod), "should fail when source fails to sync")
	require.NotContains(t, c.(*cache).sources, podGvk, "should not contain source entry in mapping")
}

func TestCacheWatchExistingSourceNotPanic(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockSrcr := NewMockSourcerer(ctrl)
	mockW := NewMockWatcher(ctrl)
	mockSrc := NewMockCloserSyncingSource(ctrl)

	c := NewCache(mockSrcr, &ocv1.ClusterExtension{}, time.Second)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)
	require.NoError(t, c.(*cache).addSource(podGvk, mockSrc))

	// In this case, a panic means there is a logic error somewhere in the
	// cache.Watch() method. It should never hit the condition where it panics
	// as it should never attempt to create a new source for one that already exists.
	require.NotPanics(t, func() { _ = c.Watch(context.Background(), mockW, pod) }, "should never panic")
}

func TestCacheWatchRemovesStaleSources(t *testing.T) {
	ctrl := gomock.NewController(t)

	podSrc := NewMockCloserSyncingSource(ctrl)
	podSrc.EXPECT().WaitForSync(gomock.Any()).Return(nil)
	podSrc.EXPECT().Close().Return(nil)

	secretSrc := NewMockCloserSyncingSource(ctrl)
	secretSrc.EXPECT().WaitForSync(gomock.Any()).Return(nil)

	mockSrcr := NewMockSourcerer(ctrl)
	gomock.InOrder(
		mockSrcr.EXPECT().Source(gomock.Any(), gomock.Any(), gomock.Any()).Return(podSrc, nil),
		mockSrcr.EXPECT().Source(gomock.Any(), gomock.Any(), gomock.Any()).Return(secretSrc, nil),
	)

	mockW := NewMockWatcher(ctrl)
	mockW.EXPECT().Watch(gomock.Any()).Return(nil).Times(2)

	c := NewCache(mockSrcr, &ocv1.ClusterExtension{}, time.Second)

	pod := &corev1.Pod{}
	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	pod.SetGroupVersionKind(podGvk)

	require.NoError(t, c.Watch(context.Background(), mockW, pod))
	require.Contains(t, c.(*cache).sources, podGvk)

	secret := &corev1.Secret{}
	secretGvk := corev1.SchemeGroupVersion.WithKind("Secret")
	secret.SetGroupVersionKind(secretGvk)
	require.NoError(t, c.Watch(context.Background(), mockW, secret))
	require.Contains(t, c.(*cache).sources, secretGvk)
	require.NotContains(t, c.(*cache).sources, podGvk)
}

func TestCacheWatchRemovingStaleSourcesError(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockSrcr := NewMockSourcerer(ctrl)

	staleSrc := NewMockCloserSyncingSource(ctrl)
	staleSrc.EXPECT().Close().Return(errors.New("error"))

	mockW := NewMockWatcher(ctrl)

	c := NewCache(mockSrcr, &ocv1.ClusterExtension{}, time.Second)

	podGvk := corev1.SchemeGroupVersion.WithKind("Pod")
	require.NoError(t, c.(*cache).addSource(podGvk, staleSrc))

	secret := &corev1.Secret{}
	secretGvk := corev1.SchemeGroupVersion.WithKind("Secret")
	secret.SetGroupVersionKind(secretGvk)
	require.Error(t, c.Watch(context.Background(), mockW, secret))
}
