package applier_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	"github.com/operator-framework/operator-controller/internal/testutil"
)

func migrationHandoffFixture(t *testing.T) (*runtime.Scheme, *ocv1.ClusterExtension, *ocv1.ClusterObjectDeployment, *ocv1.ClusterObjectSet) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "migrating",
			UID:  "current-ce",
		},
		Spec: ocv1.ClusterExtensionSpec{Namespace: "installed-namespace"},
	}
	cod := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: ext.Name,
			UID:  "current-cod",
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind)),
			},
		},
		Spec: ocv1.ClusterObjectDeploymentSpec{
			Template: ocv1.ClusterObjectDeploymentTemplate{
				Spec: ocv1.ClusterObjectDeploymentTemplateSpec{
					CollisionProtection: ocv1.CollisionProtectionPrevent,
				},
			},
		},
	}
	rev := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ext.Name + "-1",
			UID:    "migration-revision",
			Labels: map[string]string{labels.MigratedFromHelmKey: "true"},
			Annotations: map[string]string{
				labels.BundleNameKey:      "installed-bundle",
				labels.BundleVersionKey:   "1.0.0",
				labels.BundleReleaseKey:   "2",
				labels.PackageNameKey:     "installed-package",
				labels.BundleReferenceKey: "example.com/bundle:v1.0.0",
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind)),
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       "other-owner",
					UID:        "other-owner",
				},
			},
		},
		Spec: ocv1.ClusterObjectSetSpec{
			Group:               ext.Name,
			Revision:            1,
			LifecycleState:      ocv1.ClusterObjectSetLifecycleStateActive,
			CollisionProtection: ocv1.CollisionProtectionNone,
			Phases: []ocv1.ClusterObjectSetPhase{{
				Name: "deploy",
				Objects: []ocv1.ClusterObjectSetObject{{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "installed-config",
								"namespace": ext.Spec.Namespace,
							},
							"data": map[string]interface{}{"ready": "true"},
						},
					},
				}},
			}},
			ProgressionProbes: []ocv1.ProgressionProbe{{
				Selector: ocv1.ObjectSelector{
					Type:      ocv1.SelectorTypeGroupKind,
					GroupKind: metav1.GroupKind{Kind: "ConfigMap"},
				},
				Assertions: []ocv1.Assertion{{
					Type: ocv1.ProbeTypeFieldValue,
					FieldValue: ocv1.FieldValueProbe{
						FieldPath: "data.ready",
						Value:     "true",
					},
				}},
			}},
		},
		Status: ocv1.ClusterObjectSetStatus{
			CompletedAt: metav1.Now(),
			Conditions: []metav1.Condition{{
				Type:               ocv1.ClusterObjectSetTypeReady,
				Status:             metav1.ConditionTrue,
				Reason:             ocv1.ClusterObjectSetReasonAllObjectsReady,
				Message:            "installed object is ready",
				LastTransitionTime: metav1.Now(),
			}},
			ObservedPhases: []ocv1.ObservedPhase{{
				Name:               "deploy",
				Status:             ocv1.PhaseStatusReady,
				Digest:             "sha256:abcd",
				CompletedAt:        metav1.Now(),
				LastTransitionTime: metav1.Now(),
			}},
		},
	}
	cod.Spec.Template.Spec.Phases = rev.DeepCopy().Spec.Phases
	cod.Spec.Template.Spec.ProgressionProbes = rev.DeepCopy().Spec.ProgressionProbes
	return scheme, ext, cod, rev
}

func TestMigrationHandoff_Workflow(t *testing.T) {
	scheme, ext, cod, rev := migrationHandoffFixture(t)
	cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}, &ocv1.ClusterObjectDeployment{}).
		WithObjects(rev).
		Build()
	migrator := &applier.BoxcutterStorageMigrator{Client: cl, Scheme: scheme}
	getter := &controllers.BoxcutterRevisionStatesGetter{Reader: cl}

	// Before COD creation, migration must still provide the resolver's installed bundle.
	require.NoError(t, migrator.Migrate(t.Context(), ext, nil))
	states, err := getter.GetRevisionStates(t.Context(), ext)
	require.NoError(t, err)
	require.NotNil(t, states.Installed)

	// The COD cannot advance past the CE-controlled migration revision yet.
	require.NoError(t, cl.Create(t.Context(), cod))
	codReconciler := &coscontrollers.ClusterObjectDeploymentReconciler{Client: cl, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cod)}
	_, err = codReconciler.Reconcile(t.Context(), req)
	require.NoError(t, err)
	require.NoError(t, cl.Get(t.Context(), req.NamespacedName, cod))
	condition := apimeta.FindStatusCondition(cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeProgressing)
	require.NotNil(t, condition)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonOwnerConflict, condition.Reason)

	// The next CE reconcile transfers control, allowing normal COD rollout to resume.
	require.NoError(t, migrator.Migrate(t.Context(), ext, nil))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), rev))
	require.True(t, metav1.IsControlledBy(rev, cod))
	_, err = codReconciler.Reconcile(t.Context(), req)
	require.NoError(t, err)
	next := &ocv1.ClusterObjectSet{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: ext.Name + "-2"}, next))
	require.EqualValues(t, 2, next.Spec.Revision)
	require.Equal(t, ocv1.CollisionProtectionPrevent, next.Spec.CollisionProtection)
	require.True(t, metav1.IsControlledBy(next, cod))
}

func TestMigrationHandoff_PreservesRevision(t *testing.T) {
	scheme, ext, cod, rev := migrationHandoffFixture(t)
	cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		WithObjects(cod, rev).
		Build()
	before := &ocv1.ClusterObjectSet{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), before))
	migrator := &applier.BoxcutterStorageMigrator{Client: cl, Scheme: scheme}
	require.NoError(t, migrator.Migrate(t.Context(), ext, nil))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), rev))
	require.True(t, metav1.IsControlledBy(rev, cod))
	require.Equal(t, before.UID, rev.UID)
	require.Equal(t, before.Spec, rev.Spec)
	require.Equal(t, before.Status, rev.Status)
	require.Equal(t, before.Labels, rev.Labels)
	require.Equal(t, before.Annotations, rev.Annotations)
	require.Contains(t, rev.OwnerReferences, before.OwnerReferences[1])
	require.Len(t, rev.OwnerReferences, 2)
	// Direct transfer does not require labeling the revision as object-controller-created.
	require.NotContains(t, rev.Labels, labels.ManagedByKey)
}

func TestMigrationHandoff_IsIdempotent(t *testing.T) {
	scheme, ext, cod, rev := migrationHandoffFixture(t)
	cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		WithObjects(cod, rev).
		Build()
	migrator := &applier.BoxcutterStorageMigrator{Client: cl, Scheme: scheme}
	require.NoError(t, migrator.Migrate(t.Context(), ext, nil))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), rev))
	require.True(t, metav1.IsControlledBy(rev, cod))
	after := rev.DeepCopy()
	require.NoError(t, migrator.Migrate(t.Context(), ext, nil))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), rev))
	require.Equal(t, after, rev)
}

func TestMigrationHandoff_SkipsIneligibleHandoffs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ocv1.ClusterObjectDeployment, *ocv1.ClusterObjectSet)
	}{
		{
			name: "unmarked revision",
			change: func(_ *ocv1.ClusterObjectDeployment, rev *ocv1.ClusterObjectSet) {
				rev.Labels = nil
			},
		},
		{
			name: "different group",
			change: func(_ *ocv1.ClusterObjectDeployment, rev *ocv1.ClusterObjectSet) {
				rev.Spec.Group = "other-group"
			},
		},
		{
			name: "normal later revision",
			change: func(_ *ocv1.ClusterObjectDeployment, rev *ocv1.ClusterObjectSet) {
				rev.Spec.Revision = 2
			},
		},
		{
			name: "ownerless revision",
			change: func(_ *ocv1.ClusterObjectDeployment, rev *ocv1.ClusterObjectSet) {
				rev.OwnerReferences = nil
			},
		},
		{
			name: "non-controller CE reference",
			change: func(_ *ocv1.ClusterObjectDeployment, rev *ocv1.ClusterObjectSet) {
				rev.OwnerReferences[0].Controller = ptr.To(false)
			},
		},
		{
			name: "stale CE uid",
			change: func(_ *ocv1.ClusterObjectDeployment, rev *ocv1.ClusterObjectSet) {
				rev.OwnerReferences[0].UID = "old-ce"
			},
		},
		{
			name: "foreign COD",
			change: func(cod *ocv1.ClusterObjectDeployment, _ *ocv1.ClusterObjectSet) {
				cod.OwnerReferences[0].UID = "other-ce"
			},
		},
		{
			name: "ownerless COD",
			change: func(cod *ocv1.ClusterObjectDeployment, _ *ocv1.ClusterObjectSet) {
				cod.OwnerReferences = nil
			},
		},
		{
			name: "deleting COD",
			change: func(cod *ocv1.ClusterObjectDeployment, _ *ocv1.ClusterObjectSet) {
				cod.DeletionTimestamp = ptr.To(metav1.Now())
				cod.Finalizers = []string{"test.example/keep"}
			},
		},
		{
			name: "deleting revision",
			change: func(_ *ocv1.ClusterObjectDeployment, rev *ocv1.ClusterObjectSet) {
				rev.DeletionTimestamp = ptr.To(metav1.Now())
				rev.Finalizers = []string{"test.example/keep"}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme, ext, cod, rev := migrationHandoffFixture(t)
			// Completed status isolates handoff eligibility from the existing status-repair path.
			tc.change(cod, rev)
			cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
				WithStatusSubresource(&ocv1.ClusterObjectSet{}).
				WithObjects(cod, rev).
				Build()
			before := &ocv1.ClusterObjectSet{}
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), before))
			migrator := &applier.BoxcutterStorageMigrator{Client: cl, Scheme: scheme}
			// Keep a group member so even the different-group case never queries Helm.
			if rev.Spec.Group != ext.Name {
				require.NoError(t, cl.Create(t.Context(), &ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{Name: "group-member"},
					Spec:       ocv1.ClusterObjectSetSpec{Group: ext.Name, Revision: 2},
				}))
			}
			require.NoError(t, migrator.Migrate(t.Context(), ext, nil))
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), rev))
			require.Equal(t, before, rev)
		})
	}
}

func TestMigrationHandoff_OwnershipConflict(t *testing.T) {
	scheme, ext, cod, rev := migrationHandoffFixture(t)
	claimed := false
	cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		WithObjects(cod, rev).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				current := &ocv1.ClusterObjectSet{}
				require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(rev), current))
				current.OwnerReferences[0].UID = "concurrent-owner"
				require.NoError(t, cl.Update(ctx, current))
				claimed = true
				return cl.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()
	migrator := &applier.BoxcutterStorageMigrator{Client: cl, Scheme: scheme}
	err := migrator.Migrate(t.Context(), ext, nil)
	require.True(t, apierrors.IsConflict(err), "stale ownership must not be overwritten: %v", err)
	require.True(t, claimed)
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), rev))
	require.EqualValues(t, "concurrent-owner", metav1.GetControllerOf(rev).UID)
	// Retry observes the new owner and leaves it alone.
	require.NoError(t, migrator.Migrate(t.Context(), ext, nil))
}

func TestMigrationHandoff_RetriesInterruptedStatusUpdate(t *testing.T) {
	scheme, ext, cod, rev := migrationHandoffFixture(t)
	rev.Status.CompletedAt = metav1.Time{}
	statusErr := errors.New("temporary status failure")
	fail := true
	cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		WithObjects(cod, rev).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, cl client.Client, subResource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				if fail {
					return statusErr
				}
				return cl.SubResource(subResource).Update(ctx, obj, opts...)
			},
		}).
		Build()
	migrator := &applier.BoxcutterStorageMigrator{Client: cl, Scheme: scheme}
	require.ErrorIs(t, migrator.Migrate(t.Context(), ext, nil), statusErr)
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), rev))
	require.True(t, metav1.IsControlledBy(rev, ext))
	fail = false
	require.NoError(t, migrator.Migrate(t.Context(), ext, nil))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), rev))
	require.False(t, rev.Status.CompletedAt.IsZero())
	require.True(t, metav1.IsControlledBy(rev, cod))
}

func TestMigrationHandoff_RetriesInterruptedOwnershipPatch(t *testing.T) {
	scheme, ext, cod, rev := migrationHandoffFixture(t)
	patchErr := errors.New("temporary ownership patch failure")
	fail := true
	cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		WithObjects(cod, rev).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if fail {
					return patchErr
				}
				return cl.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()
	migrator := &applier.BoxcutterStorageMigrator{Client: cl, Scheme: scheme}
	require.ErrorIs(t, migrator.Migrate(t.Context(), ext, nil), patchErr)
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), rev))
	require.True(t, metav1.IsControlledBy(rev, ext))
	fail = false
	require.NoError(t, migrator.Migrate(t.Context(), ext, nil))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(rev), rev))
	require.True(t, metav1.IsControlledBy(rev, cod))
}
