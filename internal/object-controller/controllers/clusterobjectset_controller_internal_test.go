package controllers

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

func Test_ClusterObjectSetReconciler_listSiblingRevisions(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(testScheme))

	for _, tc := range []struct {
		name         string
		existingObjs func() []client.Object
		currentRev   string
		expectedRevs []string
	}{
		{
			name: "should return all active revisions except self",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				rev1 := newTestClusterObjectSetInternal(t, "rev-1")
				rev2 := newTestClusterObjectSetInternal(t, "rev-2")
				rev3 := newTestClusterObjectSetInternal(t, "rev-3")
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev2, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev3, testScheme))
				return []client.Object{ext, rev1, rev2, rev3}
			},
			currentRev:   "rev-2",
			expectedRevs: []string{"rev-1", "rev-3"},
		},
		{
			name: "should exclude archived revisions",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				rev1 := newTestClusterObjectSetInternal(t, "rev-1")
				rev2 := newTestClusterObjectSetInternal(t, "rev-2")
				rev2.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
				rev3 := newTestClusterObjectSetInternal(t, "rev-3")
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev2, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev3, testScheme))
				return []client.Object{ext, rev1, rev2, rev3}
			},
			currentRev:   "rev-3",
			expectedRevs: []string{"rev-1"},
		},
		{
			name: "should exclude deleting revisions",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				rev1 := newTestClusterObjectSetInternal(t, "rev-1")
				rev2 := newTestClusterObjectSetInternal(t, "rev-2")
				rev2.Finalizers = []string{"test-finalizer"}
				rev2.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				rev3 := newTestClusterObjectSetInternal(t, "rev-3")
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev2, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev3, testScheme))
				return []client.Object{ext, rev1, rev2, rev3}
			},
			currentRev:   "rev-3",
			expectedRevs: []string{"rev-1"},
		},
		{
			name: "should only include revisions matching owner label",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				ext2 := newTestClusterExtensionInternal()
				ext2.Name = "test-ext-2"
				ext2.UID = "test-ext-2"

				rev1 := newTestClusterObjectSetInternal(t, "rev-1")
				rev2 := newTestClusterObjectSetInternal(t, "rev-2")
				rev2.Labels[labels.OwnerNameKey] = "test-ext-2"
				rev3 := newTestClusterObjectSetInternal(t, "rev-3")
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext2, rev2, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev3, testScheme))
				return []client.Object{ext, ext2, rev1, rev2, rev3}
			},
			currentRev:   "rev-3",
			expectedRevs: []string{"rev-1"},
		},
		{
			name: "should return empty list when owner label missing",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				rev1 := newTestClusterObjectSetInternal(t, "rev-1")
				delete(rev1.Labels, labels.OwnerNameKey)
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				return []client.Object{ext, rev1}
			},
			currentRev:   "rev-1",
			expectedRevs: []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := callRevisionLister(t, testScheme, tc.existingObjs(), tc.currentRev,
				func(r *ClusterObjectSetReconciler, ctx context.Context, cos *ocv1.ClusterObjectSet) ([]*ocv1.ClusterObjectSet, error) {
					return r.listSiblingRevisions(ctx, cos)
				})
			require.ElementsMatch(t, tc.expectedRevs, result)
		})
	}
}

func Test_ClusterObjectSetReconciler_listPreviousRevisions(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(testScheme))

	for _, tc := range []struct {
		name         string
		existingObjs func() []client.Object
		currentRev   string
		expectedRevs []string
	}{
		{
			name: "should return only lower-revision active siblings",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				rev1 := newTestClusterObjectSetInternal(t, "rev-1")
				rev2 := newTestClusterObjectSetInternal(t, "rev-2")
				rev3 := newTestClusterObjectSetInternal(t, "rev-3")
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev2, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev3, testScheme))
				return []client.Object{ext, rev1, rev2, rev3}
			},
			currentRev:   "rev-2",
			expectedRevs: []string{"rev-1"},
		},
		{
			name: "should exclude higher-revision siblings",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				rev1 := newTestClusterObjectSetInternal(t, "rev-1")
				rev2 := newTestClusterObjectSetInternal(t, "rev-2")
				rev3 := newTestClusterObjectSetInternal(t, "rev-3")
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev2, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev3, testScheme))
				return []client.Object{ext, rev1, rev2, rev3}
			},
			currentRev:   "rev-1",
			expectedRevs: []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := callRevisionLister(t, testScheme, tc.existingObjs(), tc.currentRev,
				func(r *ClusterObjectSetReconciler, ctx context.Context, cos *ocv1.ClusterObjectSet) ([]*ocv1.ClusterObjectSet, error) {
					return r.listPreviousRevisions(ctx, cos)
				})
			require.ElementsMatch(t, tc.expectedRevs, result)
		})
	}
}

func callRevisionLister(
	t *testing.T,
	testScheme *runtime.Scheme,
	existingObjs []client.Object,
	currentRevName string,
	lister func(*ClusterObjectSetReconciler, context.Context, *ocv1.ClusterObjectSet) ([]*ocv1.ClusterObjectSet, error),
) []string {
	t.Helper()
	mockCtrl := gomock.NewController(t)
	testClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(existingObjs...).
		Build()

	reconciler := &ClusterObjectSetReconciler{
		Client:        testClient,
		TrackingCache: newMockTrackingCacheInternal(mockCtrl, testClient),
	}

	currentRev := &ocv1.ClusterObjectSet{}
	err := testClient.Get(t.Context(), client.ObjectKey{Name: currentRevName}, currentRev)
	require.NoError(t, err)

	result, err := lister(reconciler, t.Context(), currentRev)
	require.NoError(t, err)

	names := make([]string, 0, len(result))
	for _, rev := range result {
		names = append(names, rev.GetName())
	}
	return names
}

func newTestClusterExtensionInternal() *ocv1.ClusterExtension {
	return &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ext",
			UID:  "test-ext",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "some-namespace",
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: "my-package",
				},
			},
		},
	}
}

func newTestClusterObjectSetInternal(t *testing.T, name string) *ocv1.ClusterObjectSet {
	t.Helper()

	// Extract revision number from name (e.g., "rev-1" -> 1, "test-ext-10" -> 10)
	revNum := ExtractRevisionNumber(t, name)

	rev := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			UID:        types.UID(name),
			Generation: int64(1),
			Labels: map[string]string{
				labels.OwnerNameKey: "test-ext",
			},
		},
		Spec: ocv1.ClusterObjectSetSpec{
			Revision: revNum,
			Phases: []ocv1.ClusterObjectSetPhase{
				{
					Name:    "everything",
					Objects: []ocv1.ClusterObjectSetObject{},
				},
			},
		},
	}
	rev.SetGroupVersionKind(ocv1.GroupVersion.WithKind("ClusterObjectSet"))
	return rev
}

func newMockTrackingCacheInternal(ctrl *gomock.Controller, cl client.Client) *MockTrackingCache {
	m := NewMockTrackingCache(ctrl)
	m.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(cl.Get).AnyTimes()
	m.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(cl.List).AnyTimes()
	m.EXPECT().Source(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	m.EXPECT().Watch(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	m.EXPECT().Free(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	return m
}

func TestComputePhaseDigest(t *testing.T) {
	makeObj := func(apiVersion, kind, name string) *unstructured.Unstructured {
		return &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": apiVersion,
				"kind":       kind,
				"metadata":   map[string]interface{}{"name": name},
			},
		}
	}

	t.Run("deterministic for same content", func(t *testing.T) {
		objs := []client.Object{makeObj("v1", "ConfigMap", "cm1")}
		hash, err := computePhaseDigest("deploy", objs)
		require.NoError(t, err)
		assert.Equal(t, "sha256:e159e3f2c46b65df156d02407c44936c0fd7349149a89dadf190d27c67019edc", hash)
	})

	t.Run("deterministic with many objects", func(t *testing.T) {
		objs := make([]client.Object, 100)
		for i := range objs {
			objs[i] = makeObj("v1", "ConfigMap", fmt.Sprintf("cm-%d", i))
		}
		var first string
		for i := range 100 {
			digest, err := computePhaseDigest("deploy", objs)
			require.NoError(t, err)
			if i == 0 {
				first = digest
			} else {
				assert.Equal(t, first, digest, "digest changed on iteration %d", i)
			}
		}
	})

	t.Run("different for different object content", func(t *testing.T) {
		h1, err := computePhaseDigest("deploy", []client.Object{makeObj("v1", "ConfigMap", "cm1")})
		require.NoError(t, err)
		h2, err := computePhaseDigest("deploy", []client.Object{makeObj("v1", "ConfigMap", "cm2")})
		require.NoError(t, err)
		assert.NotEqual(t, h1, h2)
	})

	t.Run("different for different phase names", func(t *testing.T) {
		objs := []client.Object{makeObj("v1", "ConfigMap", "cm1")}
		h1, err := computePhaseDigest("deploy", objs)
		require.NoError(t, err)
		h2, err := computePhaseDigest("crds", objs)
		require.NoError(t, err)
		assert.NotEqual(t, h1, h2)
	})

	t.Run("different order produces different digest", func(t *testing.T) {
		obj1 := makeObj("v1", "ConfigMap", "cm1")
		obj2 := makeObj("v1", "ConfigMap", "cm2")
		h1, err := computePhaseDigest("deploy", []client.Object{obj1, obj2})
		require.NoError(t, err)
		h2, err := computePhaseDigest("deploy", []client.Object{obj2, obj1})
		require.NoError(t, err)
		assert.NotEqual(t, h1, h2)
	})

	t.Run("empty phase produces valid digest", func(t *testing.T) {
		hash, err := computePhaseDigest("empty", nil)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
	})

	t.Run("digest has sha256 prefix", func(t *testing.T) {
		digest, err := computePhaseDigest("deploy", []client.Object{makeObj("v1", "ConfigMap", "cm1")})
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(digest, "sha256:"), "digest should start with sha256: prefix, got %s", digest)
	})
}

func TestVerifyObservedPhases(t *testing.T) {
	t.Run("passes when digests match", func(t *testing.T) {
		stored := []ocv1.ObservedPhase{{Name: "deploy", Digest: "sha256:abc123"}}
		current := []ocv1.ObservedPhase{{Name: "deploy", Digest: "sha256:abc123"}}
		assert.NoError(t, verifyObservedPhases(stored, current))
	})

	t.Run("fails when digest changes", func(t *testing.T) {
		stored := []ocv1.ObservedPhase{{Name: "deploy", Digest: "sha256:abc123"}}
		current := []ocv1.ObservedPhase{{Name: "deploy", Digest: "sha256:def456"}}
		err := verifyObservedPhases(stored, current)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `resolved content of 1 phase(s) has changed`)
		assert.Contains(t, err.Error(), `phase "deploy"`)
	})

	t.Run("reports all mismatched phases", func(t *testing.T) {
		stored := []ocv1.ObservedPhase{
			{Name: "deploy", Digest: "sha256:aaa"},
			{Name: "crds", Digest: "sha256:bbb"},
			{Name: "rbac", Digest: "sha256:ccc"},
		}
		current := []ocv1.ObservedPhase{
			{Name: "deploy", Digest: "sha256:xxx"},
			{Name: "crds", Digest: "sha256:yyy"},
			{Name: "rbac", Digest: "sha256:ccc"},
		}
		err := verifyObservedPhases(stored, current)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `resolved content of 2 phase(s) has changed`)
		assert.Contains(t, err.Error(), `phase "deploy"`)
		assert.Contains(t, err.Error(), `phase "crds"`)
		assert.NotContains(t, err.Error(), `phase "rbac"`)
	})

	t.Run("fails when phase count changes", func(t *testing.T) {
		stored := []ocv1.ObservedPhase{{Name: "deploy", Digest: "sha256:abc123"}}
		current := []ocv1.ObservedPhase{
			{Name: "deploy", Digest: "sha256:abc123"},
			{Name: "crds", Digest: "sha256:def456"},
		}
		err := verifyObservedPhases(stored, current)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "number of phases has changed")
	})

	t.Run("fails with empty stored", func(t *testing.T) {
		current := []ocv1.ObservedPhase{{Name: "deploy", Digest: "sha256:abc123"}}
		err := verifyObservedPhases(nil, current)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpectedly empty")
	})
}

func TestVerifyReferencedSecretsImmutable(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(testScheme))
	require.NoError(t, corev1.AddToScheme(testScheme))

	immutableSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-ns",
		},
		Immutable: ptr.To(true),
		Data: map[string][]byte{
			"obj": []byte(`{"apiVersion":"v1","kind":"ConfigMap"}`),
		},
	}

	t.Run("succeeds when all secrets are immutable", func(t *testing.T) {
		testClient := fake.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(immutableSecret.DeepCopy()).
			Build()

		reconciler := &ClusterObjectSetReconciler{Client: testClient}

		cos := &ocv1.ClusterObjectSet{
			Spec: ocv1.ClusterObjectSetSpec{
				Phases: []ocv1.ClusterObjectSetPhase{{
					Name: "phase1",
					Objects: []ocv1.ClusterObjectSetObject{{
						Ref: ocv1.ObjectSourceRef{
							Name:      "test-secret",
							Namespace: "test-ns",
							Key:       "obj",
						},
					}},
				}},
			},
		}

		err := reconciler.verifyReferencedSecretsImmutable(t.Context(), cos)
		require.NoError(t, err)
	})

	t.Run("rejects non-immutable secret", func(t *testing.T) {
		mutableSecret := immutableSecret.DeepCopy()
		mutableSecret.Immutable = nil

		testClient := fake.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(mutableSecret).
			Build()

		reconciler := &ClusterObjectSetReconciler{Client: testClient}

		cos := &ocv1.ClusterObjectSet{
			Spec: ocv1.ClusterObjectSetSpec{
				Phases: []ocv1.ClusterObjectSetPhase{{
					Name: "phase1",
					Objects: []ocv1.ClusterObjectSetObject{{
						Ref: ocv1.ObjectSourceRef{
							Name:      "test-secret",
							Namespace: "test-ns",
							Key:       "obj",
						},
					}},
				}},
			},
		}

		err := reconciler.verifyReferencedSecretsImmutable(t.Context(), cos)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not immutable")
	})

	t.Run("skips phases with inline objects only", func(t *testing.T) {
		testClient := fake.NewClientBuilder().
			WithScheme(testScheme).
			Build()

		reconciler := &ClusterObjectSetReconciler{Client: testClient}

		cos := &ocv1.ClusterObjectSet{
			Spec: ocv1.ClusterObjectSetSpec{
				Phases: []ocv1.ClusterObjectSetPhase{{
					Name:    "phase1",
					Objects: []ocv1.ClusterObjectSetObject{{
						// Inline object, no ref
					}},
				}},
			},
		}

		err := reconciler.verifyReferencedSecretsImmutable(t.Context(), cos)
		require.NoError(t, err)
	})

	t.Run("checks secret only once when referenced multiple times", func(t *testing.T) {
		var secretGetCount atomic.Int32
		secret := immutableSecret.DeepCopy()
		testClient := fake.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(secret).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*corev1.Secret); ok {
						secretGetCount.Add(1)
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).
			Build()

		reconciler := &ClusterObjectSetReconciler{Client: testClient}

		cos := &ocv1.ClusterObjectSet{
			Spec: ocv1.ClusterObjectSetSpec{
				Phases: []ocv1.ClusterObjectSetPhase{{
					Name: "phase1",
					Objects: []ocv1.ClusterObjectSetObject{
						{Ref: ocv1.ObjectSourceRef{Name: "test-secret", Namespace: "test-ns", Key: "obj"}},
						{Ref: ocv1.ObjectSourceRef{Name: "test-secret", Namespace: "test-ns", Key: "obj2"}},
					},
				}},
			},
		}

		err := reconciler.verifyReferencedSecretsImmutable(t.Context(), cos)
		require.NoError(t, err)
		assert.Equal(t, int32(1), secretGetCount.Load(), "secret should be fetched only once despite multiple references")
	})
}
