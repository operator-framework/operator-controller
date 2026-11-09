package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	"github.com/operator-framework/operator-controller/internal/testutil"
)

func pendingHelmMigrationFixture(t *testing.T) (*runtime.Scheme, *ocv1.ClusterExtension, *ocv1.ClusterObjectDeployment, *ocv1.ClusterObjectSet) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "migrating",
			UID:  "current-ce",
		},
	}
	cod := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: ext.Name,
			UID:  "current-cod",
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind)),
			},
		},
	}
	rev := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ext.Name + "-1",
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
			},
		},
		Spec: ocv1.ClusterObjectSetSpec{
			Group:               ext.Name,
			Revision:            1,
			LifecycleState:      ocv1.ClusterObjectSetLifecycleStateActive,
			CollisionProtection: ocv1.CollisionProtectionNone,
		},
		Status: ocv1.ClusterObjectSetStatus{CompletedAt: metav1.Now()},
	}
	return scheme, ext, cod, rev
}

func TestBoxcutterRevisionStatesGetter_ReportsPendingHelmMigration(t *testing.T) {
	for _, deployment := range []struct {
		name    string
		present bool
	}{
		{
			name: "deployment absent",
		},
		{
			name:    "deployment awaiting handoff",
			present: true,
		},
	} {
		t.Run(deployment.name, func(t *testing.T) {
			scheme, ext, cod, rev := pendingHelmMigrationFixture(t)
			objects := []client.Object{rev}
			if deployment.present {
				objects = append(objects, cod)
			}
			cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
				WithObjects(objects...).
				Build()
			getter := &BoxcutterRevisionStatesGetter{Reader: cl}
			states, err := getter.GetRevisionStates(t.Context(), ext)
			require.NoError(t, err)
			require.Equal(t, &RevisionMetadata{
				RevisionName: "migrating-1",
				Package:      "installed-package",
				Image:        "example.com/bundle:v1.0.0",
				BundleMetadata: ocv1.BundleMetadata{
					Name:    "installed-bundle",
					Version: "1.0.0",
					Release: ptr.To("2"),
				},
			}, states.Installed)
			require.Empty(t, states.RollingOut)
		})
	}
}

func TestBoxcutterRevisionStatesGetter_FiltersIneligibleHelmMigrations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ocv1.ClusterObjectSet)
	}{
		{
			name: "missing migration marker",
			change: func(rev *ocv1.ClusterObjectSet) {
				rev.Labels = nil
			},
		},
		{
			name: "not completed",
			change: func(rev *ocv1.ClusterObjectSet) {
				rev.Status.CompletedAt = metav1.Time{}
			},
		},
		{
			name: "archived",
			change: func(rev *ocv1.ClusterObjectSet) {
				rev.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
			},
		},
		{
			name: "ownerless",
			change: func(rev *ocv1.ClusterObjectSet) {
				rev.OwnerReferences = nil
			},
		},
		{
			name: "stale CE uid",
			change: func(rev *ocv1.ClusterObjectSet) {
				rev.OwnerReferences[0].UID = "old-ce"
			},
		},
		{
			name: "non-controller CE reference",
			change: func(rev *ocv1.ClusterObjectSet) {
				rev.OwnerReferences[0].Controller = ptr.To(false)
			},
		},
		{
			name: "later revision",
			change: func(rev *ocv1.ClusterObjectSet) {
				rev.Spec.Revision = 2
			},
		},
		{
			name: "different group",
			change: func(rev *ocv1.ClusterObjectSet) {
				rev.Spec.Group = "other-group"
			},
		},
		{
			name: "deleting revision",
			change: func(rev *ocv1.ClusterObjectSet) {
				rev.DeletionTimestamp = ptr.To(metav1.Now())
				rev.Finalizers = []string{"test.example/keep"}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, deployment := range []struct {
				name    string
				present bool
			}{
				{
					name: "deployment absent",
				},
				{
					name:    "deployment awaiting handoff",
					present: true,
				},
			} {
				t.Run(deployment.name, func(t *testing.T) {
					scheme, ext, cod, rev := pendingHelmMigrationFixture(t)
					tc.change(rev)
					objects := []client.Object{rev}
					if deployment.present {
						objects = append(objects, cod)
					}
					cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
						WithObjects(objects...).
						Build()
					getter := &BoxcutterRevisionStatesGetter{Reader: cl}
					states, err := getter.GetRevisionStates(t.Context(), ext)
					require.NoError(t, err)
					require.Nil(t, states.Installed)
					require.Empty(t, states.RollingOut)
				})
			}
		})
	}
}

func TestBoxcutterRevisionStatesGetter_RejectsMigrationWithForeignDeployment(t *testing.T) {
	scheme, ext, cod, rev := pendingHelmMigrationFixture(t)
	cod.OwnerReferences[0].UID = "other-ce"
	cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
		WithObjects(cod, rev).
		Build()
	getter := &BoxcutterRevisionStatesGetter{Reader: cl}
	states, err := getter.GetRevisionStates(t.Context(), ext)
	require.NoError(t, err)
	require.Nil(t, states.Installed)
	require.Empty(t, states.RollingOut)
}
