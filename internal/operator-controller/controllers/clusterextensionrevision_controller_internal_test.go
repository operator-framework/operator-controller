package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

func Test_ClusterExtensionRevisionReconciler_listPreviousRevisions(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(testScheme))

	for _, tc := range []struct {
		name         string
		existingObjs func() []client.Object
		currentRev   string
		expectedRevs []string
	}{
		{
			// Scenario:
			//   - Three revisions belong to the same owner.
			//   - We ask for previous revisions of rev-2.
			//   - Only revisions with lower revision numbers are returned (rev-1).
			//   - Higher revision numbers (rev-3) are excluded.
			name: "should skip current revision when listing previous",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				rev1 := newTestClusterExtensionRevisionInternal(t, "rev-1")
				rev2 := newTestClusterExtensionRevisionInternal(t, "rev-2")
				rev3 := newTestClusterExtensionRevisionInternal(t, "rev-3")
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev2, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev3, testScheme))
				return []client.Object{ext, rev1, rev2, rev3}
			},
			currentRev:   "rev-2",
			expectedRevs: []string{"rev-1"},
		},
		{
			// Scenario:
			//   - One sibling is archived already.
			//   - The caller should not get archived items.
			//   - Only active siblings are returned.
			name: "should drop archived revisions when listing previous",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				rev1 := newTestClusterExtensionRevisionInternal(t, "rev-1")
				rev2 := newTestClusterExtensionRevisionInternal(t, "rev-2")
				rev2.Spec.LifecycleState = ocv1.ClusterExtensionRevisionLifecycleStateArchived
				rev3 := newTestClusterExtensionRevisionInternal(t, "rev-3")
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev2, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev3, testScheme))
				return []client.Object{ext, rev1, rev2, rev3}
			},
			currentRev:   "rev-3",
			expectedRevs: []string{"rev-1"},
		},
		{
			// Scenario:
			//   - One sibling is being deleted.
			//   - We list previous revisions.
			//   - The deleting one is filtered out.
			name: "should drop deleting revisions when listing previous",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				rev1 := newTestClusterExtensionRevisionInternal(t, "rev-1")
				rev2 := newTestClusterExtensionRevisionInternal(t, "rev-2")
				rev2.Finalizers = []string{"test-finalizer"}
				rev2.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				rev3 := newTestClusterExtensionRevisionInternal(t, "rev-3")
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev2, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev3, testScheme))
				return []client.Object{ext, rev1, rev2, rev3}
			},
			currentRev:   "rev-3",
			expectedRevs: []string{"rev-1"},
		},
		{
			// Scenario:
			//   - Two different owners have revisions.
			//   - The owner label is used as the filter.
			//   - Only siblings with the same owner come back.
			name: "should only include revisions matching owner label",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				ext2 := newTestClusterExtensionInternal()
				ext2.Name = "test-ext-2"
				ext2.UID = "test-ext-2"

				rev1 := newTestClusterExtensionRevisionInternal(t, "rev-1")
				rev2 := newTestClusterExtensionRevisionInternal(t, "rev-2")
				rev2.Labels[labels.OwnerNameKey] = "test-ext-2"
				rev3 := newTestClusterExtensionRevisionInternal(t, "rev-3")
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext2, rev2, testScheme))
				require.NoError(t, controllerutil.SetControllerReference(ext, rev3, testScheme))
				return []client.Object{ext, ext2, rev1, rev2, rev3}
			},
			currentRev:   "rev-3",
			expectedRevs: []string{"rev-1"},
		},
		{
			// Scenario:
			//   - The revision has no owner label.
			//   - Without the label we skip the lookup.
			//   - The function returns an empty list.
			name: "should return empty list when owner label missing",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtensionInternal()
				rev1 := newTestClusterExtensionRevisionInternal(t, "rev-1")
				delete(rev1.Labels, labels.OwnerNameKey)
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				return []client.Object{ext, rev1}
			},
			currentRev:   "rev-1",
			expectedRevs: []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testClient := fake.NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(tc.existingObjs()...).
				Build()

			reconciler := &ClusterExtensionRevisionReconciler{
				Client:        testClient,
				TrackingCache: &mockTrackingCacheInternal{client: testClient},
			}

			currentRev := &ocv1.ClusterExtensionRevision{}
			err := testClient.Get(t.Context(), client.ObjectKey{Name: tc.currentRev}, currentRev)
			require.NoError(t, err)

			previous, err := reconciler.listPreviousRevisions(t.Context(), currentRev)
			require.NoError(t, err)

			var names []string
			for _, rev := range previous {
				names = append(names, rev.GetName())
			}

			require.ElementsMatch(t, tc.expectedRevs, names)
		})
	}
}

func newTestClusterExtensionInternal() *ocv1.ClusterExtension {
	return &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ext",
			UID:  "test-ext",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "some-namespace",
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: "some-sa",
			},
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: "my-package",
				},
			},
		},
	}
}

func newTestClusterExtensionRevisionInternal(t *testing.T, name string) *ocv1.ClusterExtensionRevision {
	t.Helper()

	// Extract revision number from name (e.g., "rev-1" -> 1, "test-ext-10" -> 10)
	revNum := ExtractRevisionNumber(t, name)

	rev := &ocv1.ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			UID:        types.UID(name),
			Generation: int64(1),
			Labels: map[string]string{
				labels.OwnerNameKey: "test-ext",
			},
		},
		Spec: ocv1.ClusterExtensionRevisionSpec{
			Revision: revNum,
			Phases: []ocv1.ClusterExtensionRevisionPhase{
				{
					Name:    "everything",
					Objects: []ocv1.ClusterExtensionRevisionObject{},
				},
			},
		},
	}
	rev.SetGroupVersionKind(ocv1.GroupVersion.WithKind("ClusterExtensionRevision"))
	return rev
}

type mockTrackingCacheInternal struct {
	client client.Client
}

func (m *mockTrackingCacheInternal) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return m.client.Get(ctx, key, obj, opts...)
}

func (m *mockTrackingCacheInternal) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return m.client.List(ctx, list, opts...)
}

func (m *mockTrackingCacheInternal) Free(ctx context.Context, user client.Object) error {
	return nil
}

func (m *mockTrackingCacheInternal) Watch(ctx context.Context, user client.Object, gvks sets.Set[schema.GroupVersionKind]) error {
	return nil
}

func (m *mockTrackingCacheInternal) Source(h handler.EventHandler, predicates ...predicate.Predicate) source.Source {
	return nil
}
