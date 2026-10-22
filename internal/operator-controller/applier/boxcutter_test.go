package applier_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	bundlecsv "github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	bundlefs "github.com/operator-framework/operator-controller/internal/testing/bundle/fs"
	"github.com/operator-framework/operator-controller/internal/testutil"
	mockapplier "github.com/operator-framework/operator-controller/internal/testutil/mock/applier"
	mockctrlclient "github.com/operator-framework/operator-controller/internal/testutil/mock/ctrlclient"
)

var (
	dummyBundle = bundlefs.Builder().
		WithPackageName("test-package").
		WithCSV(bundlecsv.Builder().WithName("test-csv").Build()).
		Build()
)

func Test_SimpleRevisionGenerator_GenerateRevisionFromHelmRelease(t *testing.T) {
	g := &applier.SimpleRevisionGenerator{}

	helmRelease := &release.Release{
		Name: "test-123",
		Manifest: strings.Join(strings.Fields(`
		{
			"apiVersion":"v1",
			"kind":"ConfigMap",
			"metadata":{
				"finalizers":["test"],
				"ownerReferences":[{"kind":"TestOwner"}],
				"creationTimestamp":{"time":"0"},
				"uid":"1a2b3c4d",
				"resourceVersion":"12345",
				"generation":123,
				"managedFields":[{"manager":"test-manager"}],
				"deletionTimestamp":{"time":"0"},
				"deletionGracePeriodSeconds":30,
			}, "status": {
				"replicas": 3,
			}
		}`), "") + "\n" + `{"apiVersion":"v1","kind":"Secret"}` + "\n",
		Labels: map[string]string{
			labels.BundleNameKey:      "my-bundle",
			labels.PackageNameKey:     "my-package",
			labels.BundleVersionKey:   "1.2.0",
			labels.BundleReferenceKey: "bundle-ref",
		},
	}

	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-123",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "test-namespace",
		},
	}

	objectLabels := map[string]string{
		"my-label": "my-value",
	}

	rev, err := g.GenerateRevisionFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
	require.NoError(t, err)

	expected := ocv1ac.ClusterObjectSet("test-123-1").
		WithAnnotations(map[string]string{
			"olm.operatorframework.io/bundle-name":      "my-bundle",
			"olm.operatorframework.io/bundle-reference": "bundle-ref",
			"olm.operatorframework.io/bundle-version":   "1.2.0",
			"olm.operatorframework.io/package-name":     "my-package",
		}).
		WithLabels(map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: "test-123",
		}).
		WithSpec(ocv1ac.ClusterObjectSetSpec().
			WithGroup(ext.Name).
			WithLifecycleState(ocv1.ClusterObjectSetLifecycleStateActive).
			WithCollisionProtection(ocv1.CollisionProtectionNone).
			WithRevision(1).
			WithPhases(
				ocv1ac.ClusterObjectSetPhase().
					WithName("configuration").
					WithObjects(
						ocv1ac.ClusterObjectSetObject().
							WithObject(unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"labels": map[string]interface{}{
											"my-label": "my-value",
										},
										"annotations": map[string]interface{}{
											"olm.operatorframework.io/bundle-version": "1.2.0",
											"olm.operatorframework.io/package-name":   "my-package",
										},
									},
								},
							}),
						ocv1ac.ClusterObjectSetObject().
							WithObject(unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "Secret",
									"metadata": map[string]interface{}{
										"labels": map[string]interface{}{
											"my-label": "my-value",
										},
										"annotations": map[string]interface{}{
											"olm.operatorframework.io/bundle-version": "1.2.0",
											"olm.operatorframework.io/package-name":   "my-package",
										},
									},
								},
							}),
					)),
		)
	assert.Equal(t, expected.Name, rev.Name)
	assert.Equal(t, expected.Labels, rev.Labels)
	assert.Equal(t, expected.Annotations, rev.Annotations)
	assert.Equal(t, expected.Spec.LifecycleState, rev.Spec.LifecycleState)
	assert.Equal(t, expected.Spec.Group, rev.Spec.Group)
	assert.Equal(t, expected.Spec.CollisionProtection, rev.Spec.CollisionProtection)
	assert.Equal(t, expected.Spec.Revision, rev.Spec.Revision)
	assert.Equal(t, expected.Spec.Phases, rev.Spec.Phases)
}

func TestBoxcutterStorageMigrator(t *testing.T) {
	// defaultHelmRevisionResult returns the default result for GenerateRevisionFromHelmRelease in storage migrator tests.
	defaultHelmRevisionResult := func(ext *ocv1.ClusterExtension) *ocv1ac.ClusterObjectSetApplyConfiguration {
		return ocv1ac.ClusterObjectSet("test-revision").
			WithLabels(map[string]string{
				labels.OwnerNameKey: ext.Name,
			}).
			WithSpec(ocv1ac.ClusterObjectSetSpec().WithGroup(ext.Name))
	}

	// newStorageMigratorGenerator creates a gomock ClusterObjectSetGenerator for storage migrator tests.
	// GenerateRevision is not expected to be called. GenerateRevisionFromHelmRelease returns the default result.
	newStorageMigratorGenerator := func(t *testing.T) *mockapplier.MockClusterObjectSetGenerator {
		ctrl := gomock.NewController(t)
		m := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, helmRelease *release.Release, e *ocv1.ClusterExtension, objectLabels map[string]string) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
				return defaultHelmRevisionResult(e), nil
			}).AnyTimes()
		return m
	}

	t.Run("looks up only the group regardless of owner metadata", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))
		ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "migration-group", UID: "migration-group-uid"}, Spec: ocv1.ClusterExtensionSpec{Namespace: "test-namespace"}}
		other := &ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{Name: "other-group-1", Labels: map[string]string{labels.OwnerNameKey: ext.Name}},
			Spec:       ocv1.ClusterObjectSetSpec{Group: "other-group", Revision: 1},
		}
		cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(testScheme), coscontrollers.SetupIndexes).
			WithStatusSubresource(&ocv1.ClusterObjectSet{}).
			WithObjects(other).Build()
		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator: newStorageMigratorGenerator(t),
			ActionClientGetter: newMockActionGetter(gomock.NewController(t), mockActionGetterConfig{
				currentRel: &release.Release{Name: ext.Name, Info: &release.Info{Status: release.StatusDeployed}},
			}),
			Client: cl, Scheme: testScheme, FieldOwner: "test-owner",
		}
		require.NoError(t, sm.Migrate(t.Context(), ext, nil))
		rev := &ocv1.ClusterObjectSet{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "test-revision"}, rev))
		require.Equal(t, ext.Name, rev.Spec.Group)
		delete(rev.Labels, labels.OwnerNameKey)
		require.NoError(t, cl.Update(t.Context(), rev))
		// The existing revision must short-circuit migration without Helm access.
		sm.ActionClientGetter = nil
		sm.RevisionGenerator = nil
		require.NoError(t, sm.Migrate(t.Context(), ext, nil))
		foreignOwner := *metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind))
		foreignOwner.Name = "other-owner"
		foreignOwner.UID = "other-uid"
		for _, owners := range [][]metav1.OwnerReference{{foreignOwner}, nil} {
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), rev))
			rev.OwnerReferences = owners
			require.NoError(t, cl.Update(t.Context(), rev))
			require.NoError(t, sm.Migrate(t.Context(), ext, nil))
		}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(other), other))
		require.Empty(t, other.Status.Conditions)
	})

	t.Run("creates revision", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123", UID: "test123-uid"},
			Spec:       ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
		}
		ctrl := gomock.NewController(t)
		brb := newStorageMigratorGenerator(t)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{
			currentRel: &release.Release{
				Name: "test123",
				Info: &release.Info{Status: release.StatusDeployed},
			},
		})
		mockClient := mockctrlclient.NewMockClient(ctrl)
		mockStatusWriter := mockctrlclient.NewMockSubResourceWriter(ctrl)
		mockClient.EXPECT().Status().Return(mockStatusWriter).AnyTimes()

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				listOpts := (&client.ListOptions{}).ApplyOptions(opts)
				require.Equal(t, ".spec.group"+"="+ext.Name, listOpts.FieldSelector.String())
				require.Nil(t, listOpts.LabelSelector)
				return nil
			})
		mockClient.EXPECT().Apply(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
				// Verify the migration marker label is set before apply
				rev := obj.(*ocv1ac.ClusterObjectSetApplyConfiguration)
				require.Equal(t, "true", rev.Labels[labels.MigratedFromHelmKey], "Migration marker label should be set")
				require.Equal(t, ptr.To(ext.Name), rev.Spec.Group)
				require.Len(t, rev.OwnerReferences, 1)
				owner := rev.OwnerReferences[0]
				require.Equal(t, ptr.To(ext.Name), owner.Name)
				require.Equal(t, ptr.To(ext.UID), owner.UID)
				require.Equal(t, ptr.To(ocv1.ClusterExtensionKind), owner.Kind)
				require.Equal(t, ptr.To(true), owner.Controller)
				require.Equal(t, ptr.To(true), owner.BlockOwnerDeletion)
				return nil
			})
		mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate Get() returning the created revision with server-managed fields
				rev := obj.(*ocv1.ClusterObjectSet)
				rev.Name = "test-revision"
				rev.Generation = 1
				rev.ResourceVersion = "1"
				rev.Labels = map[string]string{
					labels.MigratedFromHelmKey: "true",
				}
				return nil
			})

		var updatedObj client.Object
		mockStatusWriter.EXPECT().Update(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updatedObj = obj
				return nil
			})

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		// Verify the migrated revision has completedAt set, marking it as installed
		require.NotNil(t, updatedObj, "Updated object should not be nil")

		rev, ok := updatedObj.(*ocv1.ClusterObjectSet)
		require.True(t, ok, "Updated object should be a ClusterObjectSet")

		assert.False(t, rev.Status.CompletedAt.IsZero(), "completedAt should be set on migrated revision")
	})

	t.Run("does not create revision when revisions exist", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
			Spec:       ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
		}
		// GenerateRevisionFromHelmRelease should not be called when revisions already exist
		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{})

		mockClient := mockctrlclient.NewMockClient(ctrl)

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		existingRev := ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				Labels: map[string]string{
					labels.MigratedFromHelmKey: "true",
				},
			},
			Spec: ocv1.ClusterObjectSetSpec{
				Revision: 1, // Migration creates revision 1
			},
			Status: ocv1.ClusterObjectSetStatus{
				CompletedAt: metav1.Now(),
			},
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				cosList := list.(*ocv1.ClusterObjectSetList)
				cosList.Items = []ocv1.ClusterObjectSet{existingRev}
				return nil
			})

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)
	})

	t.Run("sets status when revision exists but status is missing", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
			Spec:       ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
		}
		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{})

		mockClient := mockctrlclient.NewMockClient(ctrl)
		mockStatusWriter := mockctrlclient.NewMockSubResourceWriter(ctrl)
		mockClient.EXPECT().Status().Return(mockStatusWriter).AnyTimes()

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		existingRev := ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				Labels: map[string]string{
					labels.MigratedFromHelmKey: "true",
				},
			},
			Spec: ocv1.ClusterObjectSetSpec{
				Revision: 1, // Migration creates revision 1
			},
			// Status is empty - simulating the case where creation succeeded but status update failed
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				cosList := list.(*ocv1.ClusterObjectSetList)
				cosList.Items = []ocv1.ClusterObjectSet{existingRev}
				return nil
			})

		mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				rev := obj.(*ocv1.ClusterObjectSet)
				*rev = existingRev
				return nil
			})

		var updatedObj client.Object
		mockStatusWriter.EXPECT().Update(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updatedObj = obj
				return nil
			})

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		// Verify the status was set
		require.NotNil(t, updatedObj, "Updated object should not be nil")

		rev, ok := updatedObj.(*ocv1.ClusterObjectSet)
		require.True(t, ok, "Updated object should be a ClusterObjectSet")

		assert.False(t, rev.Status.CompletedAt.IsZero(), "completedAt should be set")
	})

	t.Run("sets completedAt for migrated revision that has not completed", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
			Spec:       ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
		}
		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{})

		mockClient := mockctrlclient.NewMockClient(ctrl)
		mockStatusWriter := mockctrlclient.NewMockSubResourceWriter(ctrl)
		mockClient.EXPECT().Status().Return(mockStatusWriter).AnyTimes()

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		// Migrated revision without completedAt (e.g., from a previous failed status update attempt).
		// This simulates a revision whose completedAt should be set during migration.
		existingRev := ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				Labels: map[string]string{
					labels.MigratedFromHelmKey: "true",
				},
			},
			Spec: ocv1.ClusterObjectSetSpec{
				Revision: 1,
			},
			// completedAt is not set - simulating a revision that was migrated but never marked completed.
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				cosList := list.(*ocv1.ClusterObjectSetList)
				cosList.Items = []ocv1.ClusterObjectSet{existingRev}
				return nil
			})

		mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				rev := obj.(*ocv1.ClusterObjectSet)
				*rev = existingRev
				return nil
			})

		var updatedObj client.Object
		mockStatusWriter.EXPECT().Update(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updatedObj = obj
				return nil
			})

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		// Verify completedAt was set
		require.NotNil(t, updatedObj, "Updated object should not be nil")

		rev, ok := updatedObj.(*ocv1.ClusterObjectSet)
		require.True(t, ok, "Updated object should be a ClusterObjectSet")

		assert.False(t, rev.Status.CompletedAt.IsZero(), "completedAt should be set during migration")
	})

	t.Run("does not set status on non-migrated revision 1", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
			Spec:       ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
		}
		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{})

		mockClient := mockctrlclient.NewMockClient(ctrl)

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		// Revision 1 created by normal Boxcutter operation (no migration label)
		// This simulates the first rollout - status should NOT be set as it may still be in progress
		existingRev := ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				// No migration label - this is a normal Boxcutter revision
			},
			Spec: ocv1.ClusterObjectSetSpec{
				Revision: 1,
			},
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				cosList := list.(*ocv1.ClusterObjectSetList)
				cosList.Items = []ocv1.ClusterObjectSet{existingRev}
				return nil
			})

		// The migration flow calls Get() to re-fetch the revision before checking its status.
		// Even for non-migrated revisions, Get() is called to determine if status needs to be set.
		mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				rev := obj.(*ocv1.ClusterObjectSet)
				*rev = existingRev
				return nil
			})

		// Status() and Update() should NOT be called for non-migrated revisions.
		// gomock will fail if any unexpected calls are made.

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)
	})

	t.Run("migrates from most recent deployed release when latest is failed", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
			Spec:       ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
		}
		expectedRelease := &release.Release{
			Name:    "test123",
			Version: 2,
			Info:    &release.Info{Status: release.StatusDeployed},
		}

		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		brb.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), expectedRelease, gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, helmRelease *release.Release, e *ocv1.ClusterExtension, objectLabels map[string]string) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
				return defaultHelmRevisionResult(e), nil
			}).Times(1)

		mag := newMockActionGetter(ctrl, mockActionGetterConfig{
			currentRel: &release.Release{
				Name:    "test123",
				Version: 3,
				Info:    &release.Info{Status: release.StatusFailed},
			},
			history: []*release.Release{
				{
					Name:    "test123",
					Version: 3,
					Info:    &release.Info{Status: release.StatusFailed},
				},
				expectedRelease,
				{
					Name:    "test123",
					Version: 1,
					Info:    &release.Info{Status: release.StatusSuperseded},
				},
			},
		})

		mockClient := mockctrlclient.NewMockClient(ctrl)
		mockStatusWriter := mockctrlclient.NewMockSubResourceWriter(ctrl)
		mockClient.EXPECT().Status().Return(mockStatusWriter).AnyTimes()

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)

		mockClient.EXPECT().Apply(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
				// Verify the migration marker label is set before apply
				rev := obj.(*ocv1ac.ClusterObjectSetApplyConfiguration)
				require.Equal(t, "true", rev.Labels[labels.MigratedFromHelmKey], "Migration marker label should be set")
				return nil
			})

		mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				rev := obj.(*ocv1.ClusterObjectSet)
				rev.Name = "test-revision"
				rev.Generation = 1
				rev.ResourceVersion = "1"
				rev.Labels = map[string]string{
					labels.MigratedFromHelmKey: "true",
				}
				return nil
			})

		var updatedObj client.Object
		mockStatusWriter.EXPECT().Update(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updatedObj = obj
				return nil
			})

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		// Verify the migrated revision has completedAt set
		require.NotNil(t, updatedObj, "Updated object should not be nil")

		rev, ok := updatedObj.(*ocv1.ClusterObjectSet)
		require.True(t, ok, "Updated object should be a ClusterObjectSet")

		assert.False(t, rev.Status.CompletedAt.IsZero(), "completedAt should be set on migrated revision")
	})

	t.Run("does not create revision when helm release is not deployed and no deployed history", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
			Spec:       ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
		}
		ctrl := gomock.NewController(t)
		// GenerateRevisionFromHelmRelease should NOT be called when no deployed release exists
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{
			currentRel: &release.Release{
				Name: "test123",
				Info: &release.Info{Status: release.StatusFailed},
			},
			history: []*release.Release{
				{
					Name:    "test123",
					Version: 2,
					Info:    &release.Info{Status: release.StatusFailed},
				},
				{
					Name:    "test123",
					Version: 1,
					Info:    &release.Info{Status: release.StatusFailed},
				},
			},
		})

		mockClient := mockctrlclient.NewMockClient(ctrl)

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)
		// gomock will verify GenerateRevisionFromHelmRelease was NOT called (no expectation set)
	})

	t.Run("does not create revision when no helm release", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
			Spec:       ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
		}
		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr: driver.ErrReleaseNotFound,
		})

		mockClient := mockctrlclient.NewMockClient(ctrl)

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)
	})

	t.Run("skips migration for managed namespace mode (empty spec.namespace)", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}
		ctrl := gomock.NewController(t)
		// A managed-namespace extension never had a Helm release, so migration must be a no-op:
		// no List, no action client, and no revision generation. No expectations are set, so
		// gomock fails the test if any of these are called.
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{})
		mockClient := mockctrlclient.NewMockClient(ctrl)

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)
	})
}

// codTestScheme returns a scheme with the types needed for COD-based Apply tests.
func codTestScheme(t *testing.T) *runtime.Scheme {
	s := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))
	return s
}

// newCODBoxcutter builds a Boxcutter backed by a real SimpleRevisionGenerator whose
// ManifestProvider renders the given objects.
func newCODBoxcutter(t *testing.T, scheme *runtime.Scheme, c client.Client, objs []client.Object) *applier.Boxcutter {
	ctrl := gomock.NewController(t)
	mp := mockapplier.NewMockManifestProvider(ctrl)
	mp.EXPECT().Get(gomock.Any(), gomock.Any()).Return(objs, nil).AnyTimes()
	return &applier.Boxcutter{
		Client: c,
		Scheme: scheme,
		RevisionGenerator: &applier.SimpleRevisionGenerator{
			Scheme:           scheme,
			ManifestProvider: mp,
			SystemNamespace:  "olmv1-system",
		},
		FieldOwner:      "test-owner",
		SystemNamespace: "olmv1-system",
	}
}

func TestBoxcutterApply_WritesClusterObjectDeployment(t *testing.T) {
	scheme := codTestScheme(t)
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ext", UID: "test-uid"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	bc := newCODBoxcutter(t, scheme, cl, []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "test-ns"}},
	})

	completed, status, err := bc.Apply(t.Context(), dummyBundle, ext, map[string]string{"some": "label"}, map[string]string{})
	require.NoError(t, err)
	require.True(t, completed)
	require.Empty(t, status)

	t.Log("by checking exactly one ClusterObjectDeployment named after the extension exists")
	codList := &ocv1.ClusterObjectDeploymentList{}
	require.NoError(t, cl.List(t.Context(), codList))
	require.Len(t, codList.Items, 1)
	cod := codList.Items[0]
	assert.Equal(t, ext.Name, cod.Name)

	t.Log("by checking the COD owner reference points to the ClusterExtension")
	require.Len(t, cod.OwnerReferences, 1)
	assert.Equal(t, ext.Name, cod.OwnerReferences[0].Name)
	assert.Equal(t, ext.UID, cod.OwnerReferences[0].UID)
	assert.Equal(t, ocv1.GroupVersion.String(), cod.OwnerReferences[0].APIVersion)
	assert.Equal(t, ocv1.ClusterExtensionKind, cod.OwnerReferences[0].Kind)
	assert.Equal(t, ptr.To(true), cod.OwnerReferences[0].Controller)
	assert.Equal(t, ptr.To(true), cod.OwnerReferences[0].BlockOwnerDeletion)

	t.Log("by checking the template phases reference packed Secrets instead of inline objects")
	require.NotEmpty(t, cod.Spec.Template.Spec.Phases)
	refNames := map[string]struct{}{}
	sawRef := false
	for _, ph := range cod.Spec.Template.Spec.Phases {
		for _, o := range ph.Objects {
			require.Nil(t, o.Object.Object, "object should be externalized to a Secret ref")
			require.NotNil(t, o.Ref)
			refNames[o.Ref.Name] = struct{}{}
			sawRef = true
		}
	}
	require.True(t, sawRef)

	t.Log("by checking the referenced Secrets exist in the system namespace owned by the ClusterExtension")
	secretList := &corev1.SecretList{}
	require.NoError(t, cl.List(t.Context(), secretList, client.InNamespace("olmv1-system")))
	require.NotEmpty(t, secretList.Items)
	for _, s := range secretList.Items {
		require.Len(t, s.OwnerReferences, 1)
		assert.Equal(t, ext.Name, s.OwnerReferences[0].Name)
		assert.Equal(t, ext.UID, s.OwnerReferences[0].UID)
		assert.Equal(t, ocv1.ClusterExtensionKind, s.OwnerReferences[0].Kind)
		assert.Equal(t, ptr.To(true), s.OwnerReferences[0].Controller)
		_, ok := refNames[s.Name]
		assert.True(t, ok, "Secret %q should be referenced by the COD", s.Name)
	}

	t.Log("by checking no ClusterObjectSet is created directly by the applier")
	cosList := &ocv1.ClusterObjectSetList{}
	require.NoError(t, cl.List(t.Context(), cosList))
	assert.Empty(t, cosList.Items)
}

func TestBoxcutterApply_NilContentUsesExistingCOD(t *testing.T) {
	scheme := codTestScheme(t)
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ext", UID: "test-uid"},
	}

	t.Run("errors when no deployment installed", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		bc := newCODBoxcutter(t, scheme, cl, nil)
		completed, status, err := bc.Apply(t.Context(), nil, ext, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "catalog content unavailable and no deployment installed")
		assert.False(t, completed)
		assert.Empty(t, status)
	})

	t.Run("maintains current state when a deployment exists", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&ocv1.ClusterObjectDeployment{ObjectMeta: metav1.ObjectMeta{Name: ext.Name}},
		).Build()
		bc := newCODBoxcutter(t, scheme, cl, nil)
		completed, status, err := bc.Apply(t.Context(), nil, ext, nil, nil)
		require.NoError(t, err)
		assert.True(t, completed)
		assert.Empty(t, status)
	})
}

func TestBoxcutterApply_UnchangedIsNoop(t *testing.T) {
	scheme := codTestScheme(t)
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ext", UID: "test-uid"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	bc := newCODBoxcutter(t, scheme, cl, []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "test-ns"}},
	})

	_, _, err := bc.Apply(t.Context(), dummyBundle, ext, nil, map[string]string{})
	require.NoError(t, err)

	first := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: ext.Name}, first))

	_, _, err = bc.Apply(t.Context(), dummyBundle, ext, nil, map[string]string{})
	require.NoError(t, err)

	second := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: ext.Name}, second))

	t.Log("by checking applying identical content twice does not bump the COD generation")
	assert.Equal(t, first.Generation, second.Generation)
}

func TestBoxcutterApply_AdoptsExistingRevisionWithoutReroll(t *testing.T) {
	scheme := codTestScheme(t)
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ext", UID: "test-uid"},
	}
	cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}, &ocv1.ClusterObjectDeployment{}).
		Build()
	bc := newCODBoxcutter(t, scheme, cl, []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "test-ns"}},
	})

	t.Log("by applying the COD for the first time to get the template")
	_, _, err := bc.Apply(t.Context(), dummyBundle, ext, nil, map[string]string{})
	require.NoError(t, err)

	t.Log("by reading the COD to extract its template")
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: ext.Name}, cod))
	require.NotNil(t, cod.Spec.Template)

	t.Log("by computing the template hash that the COD controller would compute")
	templateHash := coscontrollers.TemplateHash(cod.Spec.Template)

	t.Log("by creating a pre-existing COS in the same group with the matching template hash")
	existingCOS := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: ext.Name + "-1",
			Labels: map[string]string{
				labels.TemplateHashKey: templateHash,
			},
		},
		Spec: ocv1.ClusterObjectSetSpec{
			Group:          ext.Name,
			Revision:       1,
			LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
			Phases:         cod.Spec.Template.Spec.Phases,
		},
	}
	require.NoError(t, cl.Create(t.Context(), existingCOS))

	t.Log("by reconciling the COD controller")
	reconciler := &coscontrollers.ClusterObjectDeploymentReconciler{
		Client: cl,
		Scheme: scheme,
	}
	_, err = reconciler.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKey{Name: ext.Name}})
	require.NoError(t, err)

	t.Log("by verifying no new revision was created")
	cosList := &ocv1.ClusterObjectSetList{}
	require.NoError(t, cl.List(t.Context(), cosList))
	assert.Len(t, cosList.Items, 1, "should have exactly one COS (the pre-existing one)")
	assert.Equal(t, existingCOS.Name, cosList.Items[0].Name)

	t.Log("by verifying the existing COS was adopted (has controller owner ref pointing to the COD)")
	adoptedCOS := &ocv1.ClusterObjectSet{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(existingCOS), adoptedCOS))
	require.Len(t, adoptedCOS.OwnerReferences, 1)
	assert.Equal(t, cod.Name, adoptedCOS.OwnerReferences[0].Name)
	assert.Equal(t, cod.UID, adoptedCOS.OwnerReferences[0].UID)
	assert.Equal(t, ocv1.ClusterObjectDeploymentKind, adoptedCOS.OwnerReferences[0].Kind)
	assert.Equal(t, ptr.To(true), adoptedCOS.OwnerReferences[0].Controller)
}
