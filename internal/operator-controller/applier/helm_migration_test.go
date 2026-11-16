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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	"github.com/operator-framework/operator-controller/internal/testutil"
	mockapplier "github.com/operator-framework/operator-controller/internal/testutil/mock/applier"
	mockctrlclient "github.com/operator-framework/operator-controller/internal/testutil/mock/ctrlclient"
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
							WithType(ocv1.ObjectTypeInlineManifest).
							WithInlineManifest(unstructured.Unstructured{
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
							WithType(ocv1.ObjectTypeInlineManifest).
							WithInlineManifest(unstructured.Unstructured{
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

	// newStorageMigratorGenerator creates a gomock RevisionGenerator for storage migrator tests.
	// GenerateRevision is not expected to be called. GenerateRevisionFromHelmRelease returns the default result.
	newStorageMigratorGenerator := func(t *testing.T) *mockapplier.MockRevisionGenerator {
		ctrl := gomock.NewController(t)
		m := mockapplier.NewMockRevisionGenerator(ctrl)
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
		brb := mockapplier.NewMockRevisionGenerator(ctrl)
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
		brb := mockapplier.NewMockRevisionGenerator(ctrl)
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
		brb := mockapplier.NewMockRevisionGenerator(ctrl)
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
		brb := mockapplier.NewMockRevisionGenerator(ctrl)
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
		brb := mockapplier.NewMockRevisionGenerator(ctrl)
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
		brb := mockapplier.NewMockRevisionGenerator(ctrl)
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
		brb := mockapplier.NewMockRevisionGenerator(ctrl)
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
		brb := mockapplier.NewMockRevisionGenerator(ctrl)
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
