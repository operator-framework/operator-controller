package main

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	"github.com/operator-framework/operator-controller/internal/testutil"
)

func TestClusterObjectSetCache(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		cosLabels map[string]string
	}{
		{name: "nil labels"},
		{name: "empty labels", cosLabels: map[string]string{}},
		{name: "unrelated label", cosLabels: map[string]string{"app": "test"}},
		{name: "empty owner-name", cosLabels: map[string]string{labels.OwnerNameKey: ""}},
		{
			name: "owner labels",
			cosLabels: map[string]string{
				labels.OwnerNameKey: "test-ext",
				labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			},
		},
		{
			name:      "managed-by label",
			cosLabels: map[string]string{labels.ManagedByKey: labels.ManagedByObjectController},
		},
	}
	options := newCacheOptions("olmv1-system", true)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cos := &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ext-2", UID: "cos-uid", ResourceVersion: "123", Generation: 1,
					Labels: maps.Clone(tc.cosLabels),
					Annotations: map[string]string{
						"kubectl.kubernetes.io/last-applied-configuration": "large manifest",

						labels.BundleNameKey: "test-bundle",
					},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: ocv1.GroupVersion.String(), Kind: "ClusterObjectDeployment",
						Name: "test-ext", UID: "cod-uid", Controller: ptr.To(true),
					}},
				},
				Spec: ocv1.ClusterObjectSetSpec{
					Group: "test-ext", Revision: 2, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
					ClusterObjectSetContent: ocv1.ClusterObjectSetContent{
						Phases: []ocv1.ClusterObjectSetPhase{{Name: "install"}},
					},
				},
				Status: ocv1.ClusterObjectSetStatus{
					CompletedAt: metav1.NewTime(time.Unix(1, 0)),
					Conditions: []metav1.Condition{{
						Type: ocv1.ClusterObjectSetTypeReady, Status: metav1.ConditionTrue,
					}},
					ObjectCounts:   ocv1.ObjectCounts{Total: ptr.To(int64(1))},
					ObservedPhases: []ocv1.ObservedPhase{{Name: "install", Digest: "sha256:abc"}},
				},
			}
			before := cos.DeepCopy()
			want := &ocv1.ClusterObjectSet{
				ObjectMeta: before.ObjectMeta,
				Spec: ocv1.ClusterObjectSetSpec{
					Group: "test-ext", Revision: 2, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
				},
				Status: ocv1.ClusterObjectSetStatus{
					CompletedAt: before.Status.CompletedAt,
					Conditions:  before.Status.Conditions,
				},
			}
			delete(want.Annotations, "kubectl.kubernetes.io/last-applied-configuration")
			for obj, config := range options.ByObject {
				if _, ok := obj.(*ocv1.ClusterObjectSet); !ok {
					continue
				}
				require.NotNil(t, config.Label)
				assert.True(t, config.Label.Matches(k8slabels.Set(cos.Labels)), "group reads must see every COS")
				require.NotNil(t, config.Transform)
				actual, err := config.Transform(cos)
				require.NoError(t, err)
				assert.Equal(t, want, actual)
				return
			}
			t.Fatal("COS cache configuration is missing")
		})
	}
}

func TestClusterObjectSetCacheMigrationStatus(t *testing.T) {
	t.Parallel()
	for _, concurrentUpdate := range []bool{false, true} {
		name := "preserves uncached status"
		if concurrentUpdate {
			name = "conflicts with concurrent status update"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			scheme := runtime.NewScheme()
			require.NoError(t, ocv1.AddToScheme(scheme))
			ext := &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{Name: "test-ext"},
				Spec:       ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
			}
			rev := &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ext-1",
					Labels: map[string]string{
						labels.OwnerNameKey:        ext.Name,
						labels.MigratedFromHelmKey: "true",
					},
				},
				Spec: ocv1.ClusterObjectSetSpec{
					Group: ext.Name, Revision: 1, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
				},
				Status: ocv1.ClusterObjectSetStatus{
					Conditions: []metav1.Condition{{
						Type: ocv1.ClusterObjectSetTypeReady, Status: metav1.ConditionFalse,
						Reason: ocv1.ClusterObjectSetReasonObjectsNotReady,
					}},
					ObjectCounts: ocv1.ObjectCounts{Total: ptr.To(int64(2))},
					ObservedPhases: []ocv1.ObservedPhase{{
						Name: "install", Digest: "sha256:abc", Message: "original status",
					}},
				},
			}
			server := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
				WithStatusSubresource(&ocv1.ClusterObjectSet{}).WithObjects(rev).Build()
			cached := interceptor.NewClient(server, interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if err := cl.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					if concurrentUpdate {
						current := &ocv1.ClusterObjectSet{}
						if err := cl.Get(ctx, key, current); err != nil {
							return err
						}
						current.Status.ObservedPhases[0].Message = "concurrent status"
						if err := cl.Status().Update(ctx, current); err != nil {
							return err
						}
					}
					_, err := transformClusterObjectSet(obj)
					return err
				},
			})
			migrator := &applier.BoxcutterStorageMigrator{Client: cached}
			err := migrator.Migrate(t.Context(), ext, nil)
			actual := &ocv1.ClusterObjectSet{}
			require.NoError(t, server.Get(t.Context(), client.ObjectKeyFromObject(rev), actual))
			wantStatus := *rev.Status.DeepCopy()
			if concurrentUpdate {
				require.Error(t, err)
				assert.True(t, apierrors.IsConflict(err), "stale cache reads must conflict: %v", err)
				wantStatus.ObservedPhases[0].Message = "concurrent status"
				assert.Equal(t, wantStatus, actual.Status, "a conflict must not change server status")
				concurrentUpdate = false
				require.NoError(t, migrator.Migrate(t.Context(), ext, nil), "retry with a fresh read must succeed")
				require.NoError(t, server.Get(t.Context(), client.ObjectKeyFromObject(rev), actual))
			} else {
				require.NoError(t, err)
			}
			assert.False(t, actual.Status.CompletedAt.IsZero(), "migration must record completion")
			wantStatus.CompletedAt = actual.Status.CompletedAt
			assert.Equal(t, wantStatus, actual.Status, "migration must preserve all other status fields")
		})
	}
}

func TestClusterObjectDeploymentCache(t *testing.T) {
	t.Parallel()
	adoptionLabels := map[string]string{
		labels.ManagedByKey: labels.ManagedByOperatorController,
	}
	type testCase struct {
		name       string
		codLabels  map[string]string
		controller *metav1.OwnerReference
		retain     bool
	}
	tests := []testCase{
		{name: "unlabelled ownerless deployment stays visible"},
		{name: "managed-by label alone retains rollout data", codLabels: adoptionLabels, retain: true},
		{
			name: "mismatched owner labels do not prevent retaining rollout data",
			codLabels: map[string]string{
				labels.ManagedByKey: labels.ManagedByOperatorController,
				labels.OwnerKindKey: "Namespace",
				labels.OwnerNameKey: "other-ext",
			},
			retain: true,
		},
		{
			name: "owner labels alone do not retain rollout data",
			codLabels: map[string]string{
				labels.OwnerKindKey: ocv1.ClusterExtensionKind,
				labels.OwnerNameKey: "test-ext",
			},
		},
		{
			name:      "different managed-by value does not retain rollout data",
			codLabels: map[string]string{labels.ManagedByKey: "object-controller"},
		},
		{
			name: "matching controller without managed-by label does not retain rollout data",
			controller: &metav1.OwnerReference{
				APIVersion: ocv1.GroupVersion.String(), Kind: ocv1.ClusterExtensionKind,
				Name: "test-ext", UID: "test-uid", Controller: ptr.To(true),
			},
		},
		{
			name: "foreign controller metadata stays visible",
			controller: &metav1.OwnerReference{
				APIVersion: "v1", Kind: "Namespace", Name: "test-ext",
				UID: "foreign-uid", Controller: ptr.To(true),
			},
		},
		{
			name:      "managed-by label retains rollout data regardless of controller",
			codLabels: adoptionLabels,
			controller: &metav1.OwnerReference{
				APIVersion: "v1", Kind: "Namespace", Name: "test-ext",
				UID: "foreign-uid", Controller: ptr.To(true),
			},
			retain: true,
		},
	}
	options := newCacheOptions("olmv1-system", true)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cod := &ocv1.ClusterObjectDeployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ext", UID: "cod-uid", Labels: maps.Clone(tc.codLabels),
					Annotations: map[string]string{
						"kubectl.kubernetes.io/last-applied-configuration": "large manifest",
						"olm.operatorframework.io/content-hash":            "keep-me",
					},
				},
				Spec: ocv1.ClusterObjectDeploymentSpec{RevisionHistoryLimit: ptr.To(int32(5))},
				Status: ocv1.ClusterObjectDeploymentStatus{Conditions: []metav1.Condition{{
					Type: ocv1.ClusterObjectDeploymentTypeAvailable, Status: metav1.ConditionTrue,
				}}},
			}
			if tc.controller != nil {
				cod.OwnerReferences = []metav1.OwnerReference{*tc.controller}
			}
			before := cod.DeepCopy()
			for obj, config := range options.ByObject {
				if _, ok := obj.(*ocv1.ClusterObjectDeployment); !ok {
					continue
				}
				require.NotNil(t, config.Label)
				assert.True(t, config.Label.Matches(k8slabels.Set(cod.Labels)), "name-based reads must see every COD")
				require.NotNil(t, config.Transform)
				transformed, err := config.Transform(cod)
				require.NoError(t, err)
				actual, ok := transformed.(*ocv1.ClusterObjectDeployment)
				require.True(t, ok)
				assert.Equal(t, before.Labels, actual.Labels)
				assert.Equal(t, before.OwnerReferences, actual.OwnerReferences)
				assert.Equal(t, before.Name, actual.Name)
				assert.Equal(t, before.UID, actual.UID)
				assert.Equal(t, map[string]string{"olm.operatorframework.io/content-hash": "keep-me"}, actual.Annotations)
				if tc.retain {
					assert.Equal(t, before.Spec, actual.Spec)
					assert.Equal(t, before.Status, actual.Status)
				} else {
					assert.Empty(t, actual.Spec)
					assert.Empty(t, actual.Status)
				}
				return
			}
			t.Fatal("COD cache configuration is missing")
		})
	}
}
