package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	mockapplier "github.com/operator-framework/operator-controller/internal/testutil/mock/applier"
	mockctrlclient "github.com/operator-framework/operator-controller/internal/testutil/mock/ctrlclient"
	mockhelmclient "github.com/operator-framework/operator-controller/internal/testutil/mock/helmclient"
)

func TestMigrateStorageGatesFollowingSteps(t *testing.T) {
	for _, tc := range []struct {
		name               string
		stage              string
		wantFollowingSteps int
		wantRequeueAfter   time.Duration
		wantErr            error
	}{
		{
			name: "wait for adoption", stage: labels.HelmMigrationAdopting,
			wantFollowingSteps: 0, wantRequeueAfter: 10 * time.Second, wantErr: nil,
		},
		{
			name: "wait for protection", stage: labels.HelmMigrationProtecting,
			wantFollowingSteps: 0, wantRequeueAfter: 10 * time.Second, wantErr: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a pending migration and a subsequent ordinary reconciliation step.
			ext, cod := migrationStatusFixture(tc.stage)
			scheme := runtime.NewScheme()
			require.NoError(t, ocv1.AddToScheme(scheme))
			stored := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cod).Build()
			mockCtrl := gomock.NewController(t)
			api := mockctrlclient.NewMockClient(mockCtrl)
			api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
			m := &applier.BoxcutterStorageMigrator{
				Client:             api,
				ActionClientGetter: mockhelmclient.NewMockActionClientGetter(mockCtrl),
				RevisionGenerator:  mockapplier.NewMockRevisionGenerator(mockCtrl),
			}
			followingSteps := 0
			steps := ReconcileSteps{
				MigrateStorage(m),
				func(context.Context, *reconcileState, *ocv1.ClusterExtension) (*ctrl.Result, error) {
					followingSteps++
					return nil, nil
				},
			}

			// When the CE pipeline reconciles; Helm, generation, and API writes are forbidden.
			result, err := steps.Reconcile(t.Context(), ext)

			// Then normal resolution, unpacking, and application remain gated until retry.
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantFollowingSteps, followingSteps)
			require.Equal(t, tc.wantRequeueAfter, result.RequeueAfter)
		})
	}
}

func TestMigrateStorageReportsMigrationError(t *testing.T) {
	// Given an API error while reading the migration deployment.
	ext, cod := migrationStatusFixture(labels.HelmMigrationAdopting)
	lookupFailure := errors.New("API unavailable")
	mockCtrl := gomock.NewController(t)
	api := mockctrlclient.NewMockClient(mockCtrl)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).Return(lookupFailure)
	m := &applier.BoxcutterStorageMigrator{
		Client:             api,
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(mockCtrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(mockCtrl),
	}

	// When the migration step fails; Helm, generation, and API writes are forbidden.
	result, err := MigrateStorage(m)(t.Context(), &reconcileState{}, ext)

	// Then the original error reaches the caller and the CE reports why it is retrying.
	require.ErrorIs(t, err, lookupFailure)
	require.Nil(t, result)
	progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, progress)
	require.Equal(t, metav1.ConditionTrue, progress.Status)
	require.Equal(t, ocv1.ReasonRetrying, progress.Reason)
	require.Equal(t, ext.Generation, progress.ObservedGeneration)
	require.Equal(t, "migrating storage: getting migration deployment: API unavailable", progress.Message)
}

func TestMigrationStatusUpdateFailureResumesPersistedDeployment(t *testing.T) {
	// Given a stored protecting COD and a CE status write that fails once.
	ext, cod := migrationStatusFixture(labels.HelmMigrationProtecting)
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))
	stored := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(ext, cod).WithObjects(ext, cod).Build()
	mockCtrl := gomock.NewController(t)
	api := mockctrlclient.NewMockClient(mockCtrl)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(ext), gomock.AssignableToTypeOf(ext)).DoAndReturn(stored.Get).Times(2)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.AssignableToTypeOf(cod)).DoAndReturn(stored.Get).Times(2)
	status := mockctrlclient.NewMockStatusWriter(mockCtrl)
	api.EXPECT().Status().Return(status).Times(2)
	gomock.InOrder(
		status.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(ext)).Return(errors.New("status unavailable")),
		status.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(ext)).DoAndReturn(stored.Status().Update),
	)
	m := &applier.BoxcutterStorageMigrator{
		Client:             api,
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(mockCtrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(mockCtrl),
	}
	r := &ClusterExtensionReconciler{Client: api, ReconcileSteps: ReconcileSteps{MigrateStorage(m)}}

	// When the first reconcile cannot persist the CE status.
	_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ext)})

	// Then the write failure reaches the caller.
	require.ErrorContains(t, err, "status unavailable")

	// Given reconstructed dependencies, only CE/COD reads and the CE status retry are allowed.
	restarted := &applier.BoxcutterStorageMigrator{
		Client:             api,
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(mockCtrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(mockCtrl),
	}
	r = &ClusterExtensionReconciler{Client: api, ReconcileSteps: ReconcileSteps{MigrateStorage(restarted)}}

	// When the controller retries after reconstruction.
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ext)})

	// Then it reports the stored identity and stage without Helm, generation, or COD writes.
	require.NoError(t, err)
	require.Equal(t, 10*time.Second, result.RequeueAfter)
	actual := &ocv1.ClusterExtension{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(ext), actual))
	require.NotNil(t, actual.Status.Install)
	require.Equal(t, "stored.v1.0.0", actual.Status.Install.Bundle.Name)
	persisted := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(cod), persisted))
	require.Equal(t, cod.Spec, persisted.Spec)
	require.Equal(t, labels.HelmMigrationProtecting, persisted.Annotations[labels.HelmMigrationKey])
}

func TestMigrationStatusPreservesHelmIdentityWithoutReadiness(t *testing.T) {
	for _, tc := range []struct {
		name                string
		previousInstall     *ocv1.ClusterExtensionInstallStatus
		wantBundle          ocv1.BundleMetadata
		wantInstalledStatus metav1.ConditionStatus
		wantInstalledImage  string
		wantAvailable       *metav1.Condition
	}{
		{
			name: "initialize identity from Helm", previousInstall: nil,
			wantBundle:          ocv1.BundleMetadata{Name: "stored.v1.0.0", Version: "1.0.0", Release: ptr.To("")},
			wantInstalledStatus: metav1.ConditionTrue, wantInstalledImage: "image:stored", wantAvailable: nil,
		},
		{
			name: "retain previously installed identity",
			previousInstall: &ocv1.ClusterExtensionInstallStatus{
				Bundle: ocv1.BundleMetadata{Name: "stored.v1.0.0", Version: "1.0.0", Release: ptr.To("")},
			},
			wantBundle:          ocv1.BundleMetadata{Name: "stored.v1.0.0", Version: "1.0.0", Release: ptr.To("")},
			wantInstalledStatus: metav1.ConditionTrue, wantInstalledImage: "image:stored", wantAvailable: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given the stored Helm identity, with no COD readiness observation.
			ext, cod := migrationStatusFixture(labels.HelmMigrationAdopting)
			ext.Status.Install = tc.previousInstall.DeepCopy()

			// When migration status is reflected onto the CE; this function performs no I/O.
			setMigrationStatus(ext, cod)

			// Then installation provenance is retained without inventing availability.
			require.NotNil(t, ext.Status.Install)
			require.Equal(t, tc.wantBundle, ext.Status.Install.Bundle)
			installed := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
			require.NotNil(t, installed)
			require.Equal(t, tc.wantInstalledStatus, installed.Status)
			require.Contains(t, installed.Message, tc.wantInstalledImage)
			require.Equal(t, tc.wantAvailable, apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeAvailable))
		})
	}
}

func TestMigrationStatusReportsRealAvailability(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		available              metav1.ConditionStatus
		wantAvailable          metav1.ConditionStatus
		wantAvailableReason    string
		wantAvailableMessage   string
		wantObservedGeneration int64
		wantProgressReason     string
	}{
		{
			name: "healthy resources", available: metav1.ConditionTrue,
			wantAvailable: metav1.ConditionTrue, wantAvailableReason: ocv1.ReasonProbesSucceeded,
			wantAvailableMessage:   "Objects are available and pass all probes.",
			wantObservedGeneration: 5, wantProgressReason: ocv1.ReasonRollingOut,
		},
		{
			name: "unhealthy resources", available: metav1.ConditionFalse,
			wantAvailable: metav1.ConditionFalse, wantAvailableReason: "ObservedHealth",
			wantAvailableMessage:   "live probes",
			wantObservedGeneration: 5, wantProgressReason: ocv1.ReasonRollingOut,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given actual health reported by the COD during protection.
			ext, cod := migrationStatusFixture(labels.HelmMigrationProtecting)
			cod.Status.Conditions = []metav1.Condition{{
				Type: ocv1.ClusterObjectDeploymentTypeAvailable, Status: tc.available,
				Reason: "ObservedHealth", Message: "live probes",
			}}

			// When migration status is reflected onto the CE.
			setMigrationStatus(ext, cod)

			// Then the CE reports observed health and continues to report migration progress.
			available := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeAvailable)
			require.NotNil(t, available)
			require.Equal(t, tc.wantAvailable, available.Status)
			require.Equal(t, tc.wantAvailableReason, available.Reason)
			require.Equal(t, tc.wantAvailableMessage, available.Message)
			require.Equal(t, tc.wantObservedGeneration, available.ObservedGeneration)
			progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
			require.NotNil(t, progress)
			require.Equal(t, tc.wantProgressReason, progress.Reason)
		})
	}
}

func TestMigrationStatusMapsCurrentFailures(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		codReason              string
		wantReason             string
		wantStatus             metav1.ConditionStatus
		wantObservedGeneration int64
		wantMessage            string
	}{
		{
			name: "collision", codReason: ocv1.ClusterObjectDeploymentReasonRevisionObjectCollision,
			wantReason: ocv1.ReasonRetrying, wantStatus: metav1.ConditionTrue,
			wantObservedGeneration: 5, wantMessage: "Helm migration Protecting: blocking detail",
		},
		{
			name: "precondition", codReason: ocv1.ClusterObjectDeploymentReasonRevisionPreconditionsNotMet,
			wantReason: ocv1.ReasonRetrying, wantStatus: metav1.ConditionTrue,
			wantObservedGeneration: 5, wantMessage: "Helm migration Protecting: blocking detail",
		},
		{
			name: "deadline", codReason: ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded,
			wantReason: ocv1.ReasonProgressDeadlineExceeded, wantStatus: metav1.ConditionFalse,
			wantObservedGeneration: 5, wantMessage: "Helm migration Protecting: blocking detail",
		},
		{
			name: "invalid", codReason: ocv1.ClusterObjectDeploymentReasonInvalidSpecification,
			wantReason: ocv1.ReasonBlocked, wantStatus: metav1.ConditionFalse,
			wantObservedGeneration: 5, wantMessage: "Helm migration Protecting: blocking detail",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a failure observed at the COD's current generation.
			ext, cod := migrationStatusFixture(labels.HelmMigrationProtecting)
			cod.Status.Conditions = []metav1.Condition{{
				Type: ocv1.ClusterObjectDeploymentTypeProgressing, Status: metav1.ConditionFalse,
				Reason: tc.codReason, Message: "blocking detail", ObservedGeneration: cod.Generation,
			}}

			// When migration status is reflected onto the CE.
			setMigrationStatus(ext, cod)

			// Then the CE exposes the appropriate retry or terminal reason and blocking detail.
			progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
			require.NotNil(t, progress)
			require.Equal(t, tc.wantReason, progress.Reason)
			require.Equal(t, tc.wantStatus, progress.Status)
			require.Equal(t, tc.wantObservedGeneration, progress.ObservedGeneration)
			require.Equal(t, tc.wantMessage, progress.Message)
		})
	}
}

func TestMigrationStatusIgnoresStaleProgress(t *testing.T) {
	for _, tc := range []struct {
		name       string
		codReason  string
		wantReason string
		wantStatus metav1.ConditionStatus
	}{
		{
			name: "old completion", codReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
			wantReason: ocv1.ReasonRollingOut, wantStatus: metav1.ConditionTrue,
		},
		{
			name: "old deadline failure", codReason: ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded,
			wantReason: ocv1.ReasonRollingOut, wantStatus: metav1.ConditionTrue,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given progress observed before the current COD generation.
			ext, cod := migrationStatusFixture(labels.HelmMigrationProtecting)
			cod.Status.Conditions = []metav1.Condition{{
				Type: ocv1.ClusterObjectDeploymentTypeProgressing, Status: metav1.ConditionFalse,
				Reason: tc.codReason, ObservedGeneration: cod.Generation - 1,
			}}

			// When migration status is reflected onto the CE.
			setMigrationStatus(ext, cod)

			// Then stale completion or failure does not replace the current rollout progress.
			progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
			require.NotNil(t, progress)
			require.Equal(t, tc.wantReason, progress.Reason)
			require.Equal(t, tc.wantStatus, progress.Status)
		})
	}
}

func TestMigrationTransitionReportsProgressInsteadOfSuccess(t *testing.T) {
	// Given protection has just started but the COD still reports adoption completion.
	ext, cod := migrationStatusFixture(labels.HelmMigrationProtecting)
	cod.Status.Conditions = []metav1.Condition{{
		Type: ocv1.ClusterObjectDeploymentTypeProgressing, Status: metav1.ConditionFalse,
		Reason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, ObservedGeneration: cod.Generation,
	}}

	// When migration status is reflected before the COD controller observes the new stage.
	setMigrationStatus(ext, cod)

	// Then the CE reports ongoing migration rather than overall success between stages.
	progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, progress)
	require.Equal(t, ocv1.ReasonRollingOut, progress.Reason)
	require.Equal(t, metav1.ConditionTrue, progress.Status)
}

// migrationStatusFixture returns fresh, unstored CE/COD data for bundle 1.0.0.
// The CE is generation 5 and owns a generation-2 COD. stage sets the migration
// marker and matching collision protection; no rollout conditions are supplied.
func migrationStatusFixture(stage string) (*ocv1.ClusterExtension, *ocv1.ClusterObjectDeployment) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "migration", UID: "current-extension", Generation: 5}}
	cod := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: ext.Name, UID: "deployment", Generation: 2,
			Annotations:     map[string]string{labels.HelmMigrationKey: stage},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind))},
		},
		Spec: ocv1.ClusterObjectDeploymentSpec{Template: ocv1.ClusterObjectDeploymentTemplate{
			Metadata: ocv1.ClusterObjectDeploymentTemplateMetadata{Annotations: map[string]string{
				labels.BundleNameKey: "stored.v1.0.0", labels.BundleVersionKey: "1.0.0", labels.BundleReferenceKey: "image:stored", labels.BundleReleaseKey: "",
			}},
			Spec: ocv1.ClusterObjectDeploymentTemplateSpec{
				ClusterObjectSetContent: ocv1.ClusterObjectSetContent{CollisionProtection: ocv1.CollisionProtectionNone},
			},
		}},
	}
	if stage == labels.HelmMigrationProtecting {
		cod.Spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionPrevent
	}
	return ext, cod
}
