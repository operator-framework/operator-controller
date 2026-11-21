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
	type input struct {
		deploymentConditions []metav1.Condition
		protection           ocv1.CollisionProtection
	}
	type expected struct {
		followingSteps int
		requeueAfter   time.Duration
		err            error
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "wait for adoption",
			input: input{
				deploymentConditions: nil,
				protection:           ocv1.CollisionProtectionNone,
			},
			want: expected{
				followingSteps: 0,
				requeueAfter:   10 * time.Second,
				err:            nil,
			},
		},
		{
			name: "wait for protection",
			input: input{
				deploymentConditions: nil,
				protection:           ocv1.CollisionProtectionPrevent,
			},
			want: expected{
				followingSteps: 0,
				requeueAfter:   10 * time.Second,
				err:            nil,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given a pending migration and a subsequent ordinary reconciliation step.
			ext, cod := migrationStatusFixture(tc.input.protection)
			cod.Status.Conditions = tc.input.deploymentConditions
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
			require.ErrorIs(t, err, tc.want.err)
			require.Equal(t, tc.want.followingSteps, followingSteps)
			require.Equal(t, tc.want.requeueAfter, result.RequeueAfter)
		})
	}
}

func TestMigrateStorageWaitsWhenRevisionAppearsBeforeItsDeployment(t *testing.T) {
	ext, cod := migrationStatusFixture(ocv1.CollisionProtectionNone)
	ext.Spec.Namespace = "installed"
	ext.Status.Install = &ocv1.ClusterExtensionInstallStatus{Bundle: ocv1.BundleMetadata{Name: "installed.v1"}}
	revision := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{Name: cod.Name + "-1", OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(cod, ocv1.GroupVersion.WithKind(ocv1.ClusterObjectDeploymentKind)),
		}},
		Spec: ocv1.ClusterObjectSetSpec{Group: cod.Name, Revision: 1},
	}

	wantStatus := ext.Status.DeepCopy()
	wantRequeue := 10 * time.Second
	wantFollowingSteps := 0

	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(revision).
		WithIndex(&ocv1.ClusterObjectSet{}, ".spec.group", func(obj client.Object) []string {
			return []string{obj.(*ocv1.ClusterObjectSet).Spec.Group}
		}).Build()
	followingSteps := 0
	steps := ReconcileSteps{
		MigrateStorage(&applier.BoxcutterStorageMigrator{Client: reader}),
		func(context.Context, *reconcileState, *ocv1.ClusterExtension) (*ctrl.Result, error) {
			followingSteps++
			return nil, nil
		},
	}

	result, err := steps.Reconcile(t.Context(), ext)
	require.NoError(t, err)
	require.Equal(t, wantFollowingSteps, followingSteps)
	require.Equal(t, wantRequeue, result.RequeueAfter)
	require.Equal(t, *wantStatus, ext.Status)
}

func TestMigrateStorageReportsMigrationError(t *testing.T) {
	input := struct {
		protection          ocv1.CollisionProtection
		extensionGeneration int64
		lookupErr           error
	}{
		protection:          ocv1.CollisionProtectionNone,
		extensionGeneration: 5,
		lookupErr:           errors.New("API unavailable"),
	}
	want := struct {
		err         error
		result      *ctrl.Result
		progressing metav1.Condition
	}{
		err:    input.lookupErr,
		result: nil,
		progressing: metav1.Condition{
			Type:               ocv1.TypeProgressing,
			Status:             metav1.ConditionTrue,
			Reason:             ocv1.ReasonRetrying,
			Message:            "migrating storage: getting migration deployment: API unavailable",
			ObservedGeneration: 5,
		},
	}

	// Given an API error while reading the migration deployment.
	ext, cod := migrationStatusFixture(input.protection)
	ext.Generation = input.extensionGeneration
	mockCtrl := gomock.NewController(t)
	api := mockctrlclient.NewMockClient(mockCtrl)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).Return(input.lookupErr)
	m := &applier.BoxcutterStorageMigrator{
		Client:             api,
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(mockCtrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(mockCtrl),
	}

	// When the migration step fails; Helm, generation, and API writes are forbidden.
	result, err := MigrateStorage(m)(t.Context(), &reconcileState{}, ext)

	// Then the original error reaches the caller and the CE reports why it is retrying.
	require.ErrorIs(t, err, want.err)
	require.Equal(t, want.result, result)
	progress := apimeta.FindStatusCondition(ext.Status.Conditions, want.progressing.Type)
	require.NotNil(t, progress)
	require.Equal(t, want.progressing.Status, progress.Status)
	require.Equal(t, want.progressing.Reason, progress.Reason)
	require.Equal(t, want.progressing.ObservedGeneration, progress.ObservedGeneration)
	require.Equal(t, want.progressing.Message, progress.Message)
}

func TestMigrationStatusUpdateFailureResumesPersistedDeployment(t *testing.T) {
	input := struct {
		protection      ocv1.CollisionProtection
		statusUpdateErr error
	}{
		protection:      ocv1.CollisionProtectionPrevent,
		statusUpdateErr: errors.New("status unavailable"),
	}

	// Given a stored protecting COD and a CE status write that fails once.
	ext, cod := migrationStatusFixture(input.protection)
	want := struct {
		firstErrContains string
		retryErr         error
		requeueAfter     time.Duration
		bundleName       string
		protection       ocv1.CollisionProtection
		deploymentSpec   ocv1.ClusterObjectDeploymentSpec
		attempts         int
	}{
		firstErrContains: "status unavailable",
		retryErr:         nil,
		requeueAfter:     10 * time.Second,
		bundleName:       "stored.v1.0.0",
		protection:       ocv1.CollisionProtectionPrevent,
		deploymentSpec:   *cod.Spec.DeepCopy(),
		attempts:         2,
	}
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))
	stored := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(ext, cod).WithObjects(ext, cod).Build()
	mockCtrl := gomock.NewController(t)
	api := mockctrlclient.NewMockClient(mockCtrl)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(ext), gomock.AssignableToTypeOf(ext)).DoAndReturn(stored.Get).Times(want.attempts)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.AssignableToTypeOf(cod)).DoAndReturn(stored.Get).Times(want.attempts)
	status := mockctrlclient.NewMockStatusWriter(mockCtrl)
	api.EXPECT().Status().Return(status).Times(want.attempts)
	gomock.InOrder(
		status.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(ext)).Return(input.statusUpdateErr),
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
	require.ErrorContains(t, err, want.firstErrContains)

	// Given reconstructed dependencies, only CE/COD reads and the CE status retry are allowed.
	restarted := &applier.BoxcutterStorageMigrator{
		Client:             api,
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(mockCtrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(mockCtrl),
	}
	r = &ClusterExtensionReconciler{Client: api, ReconcileSteps: ReconcileSteps{MigrateStorage(restarted)}}

	// When the controller retries after reconstruction.
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ext)})

	// Then it reports the stored identity and protection without Helm, generation, or COD writes.
	require.ErrorIs(t, err, want.retryErr)
	require.Equal(t, want.requeueAfter, result.RequeueAfter)
	actual := &ocv1.ClusterExtension{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(ext), actual))
	require.NotNil(t, actual.Status.Install)
	require.Equal(t, want.bundleName, actual.Status.Install.Bundle.Name)
	persisted := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(cod), persisted))
	require.Equal(t, want.deploymentSpec, persisted.Spec)
	require.Equal(t, want.protection, persisted.Spec.Template.Spec.CollisionProtection)
}

func TestMigrationStatusPreservesHelmIdentityWithoutReadiness(t *testing.T) {
	storedBundleAnnotations := map[string]string{
		labels.BundleNameKey:      "stored.v1.0.0",
		labels.BundleVersionKey:   "1.0.0",
		labels.BundleReferenceKey: "image:stored",
		labels.BundleReleaseKey:   "",
	}

	type input struct {
		deploymentConditions []metav1.Condition
		protection           ocv1.CollisionProtection
		bundleAnnotations    map[string]string
		previousInstall      *ocv1.ClusterExtensionInstallStatus
	}
	type expected struct {
		bundle          ocv1.BundleMetadata
		installedStatus metav1.ConditionStatus
		installedImage  string
		available       *metav1.Condition
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "initialize identity from Helm",
			input: input{
				deploymentConditions: nil,
				protection:           ocv1.CollisionProtectionNone,
				bundleAnnotations:    storedBundleAnnotations,
				previousInstall:      nil,
			},
			want: expected{
				bundle:          ocv1.BundleMetadata{Name: "stored.v1.0.0", Version: "1.0.0", Release: ptr.To("")},
				installedStatus: metav1.ConditionTrue,
				installedImage:  "image:stored",
				available:       nil,
			},
		},
		{
			name: "retain previously installed identity",
			input: input{
				deploymentConditions: nil,
				protection:           ocv1.CollisionProtectionNone,
				bundleAnnotations:    storedBundleAnnotations,
				previousInstall: &ocv1.ClusterExtensionInstallStatus{
					Bundle: ocv1.BundleMetadata{Name: "stored.v1.0.0", Version: "1.0.0", Release: ptr.To("")},
				},
			},
			want: expected{
				bundle:          ocv1.BundleMetadata{Name: "stored.v1.0.0", Version: "1.0.0", Release: ptr.To("")},
				installedStatus: metav1.ConditionTrue,
				installedImage:  "image:stored",
				available:       nil,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given the stored Helm identity, with no COD readiness observation.
			ext, cod := migrationStatusFixture(tc.input.protection)
			cod.Status.Conditions = tc.input.deploymentConditions
			cod.Spec.Template.Metadata.Annotations = tc.input.bundleAnnotations
			ext.Status.Install = tc.input.previousInstall.DeepCopy()

			// When migration status is reflected onto the CE; this function performs no I/O.
			setMigrationStatus(ext, cod)

			// Then installation provenance is retained without inventing availability.
			require.NotNil(t, ext.Status.Install)
			require.Equal(t, tc.want.bundle, ext.Status.Install.Bundle)
			installed := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
			require.NotNil(t, installed)
			require.Equal(t, tc.want.installedStatus, installed.Status)
			require.Contains(t, installed.Message, tc.want.installedImage)
			require.Equal(t, tc.want.available, apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeAvailable))
		})
	}
}

func TestMigrationStatusReportsRealAvailability(t *testing.T) {
	type input struct {
		protection          ocv1.CollisionProtection
		extensionGeneration int64
		available           metav1.Condition
	}
	type expected struct {
		available         metav1.Condition
		progressingReason string
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "healthy resources",
			input: input{
				protection:          ocv1.CollisionProtectionPrevent,
				extensionGeneration: 5,
				available: metav1.Condition{
					Type:    ocv1.ClusterObjectDeploymentTypeAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "ObservedHealth",
					Message: "live probes",
				},
			},
			want: expected{
				available: metav1.Condition{
					Type:               ocv1.TypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ReasonProbesSucceeded,
					Message:            "Objects are available and pass all probes.",
					ObservedGeneration: 5,
				},
				progressingReason: ocv1.ReasonRollingOut,
			},
		},
		{
			name: "unhealthy resources",
			input: input{
				protection:          ocv1.CollisionProtectionPrevent,
				extensionGeneration: 5,
				available: metav1.Condition{
					Type:    ocv1.ClusterObjectDeploymentTypeAvailable,
					Status:  metav1.ConditionFalse,
					Reason:  "ObservedHealth",
					Message: "live probes",
				},
			},
			want: expected{
				available: metav1.Condition{
					Type:               ocv1.TypeAvailable,
					Status:             metav1.ConditionFalse,
					Reason:             "ObservedHealth",
					Message:            "live probes",
					ObservedGeneration: 5,
				},
				progressingReason: ocv1.ReasonRollingOut,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given the migration protection, CE generation, and COD health declared above.
			ext, cod := migrationStatusFixture(tc.input.protection)
			ext.Generation = tc.input.extensionGeneration
			cod.Status.Conditions = []metav1.Condition{tc.input.available}

			// When migration status is reflected onto the CE.
			setMigrationStatus(ext, cod)

			// Then availability and migration progress match the expected results.
			available := apimeta.FindStatusCondition(ext.Status.Conditions, tc.want.available.Type)
			require.NotNil(t, available)
			require.Equal(t, tc.want.available.Status, available.Status)
			require.Equal(t, tc.want.available.Reason, available.Reason)
			require.Equal(t, tc.want.available.Message, available.Message)
			require.Equal(t, tc.want.available.ObservedGeneration, available.ObservedGeneration)
			progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
			require.NotNil(t, progress)
			require.Equal(t, tc.want.progressingReason, progress.Reason)
		})
	}
}

func TestMigrationStatusMapsCurrentFailures(t *testing.T) {
	type input struct {
		protection           ocv1.CollisionProtection
		extensionGeneration  int64
		deploymentGeneration int64
		progressing          metav1.Condition
	}
	type expected struct {
		reason             string
		status             metav1.ConditionStatus
		observedGeneration int64
		message            string
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "collision",
			input: input{
				protection:           ocv1.CollisionProtectionPrevent,
				extensionGeneration:  5,
				deploymentGeneration: 2,
				progressing: metav1.Condition{
					Type:   ocv1.ClusterObjectDeploymentTypeProgressing,
					Status: metav1.ConditionFalse,
					Reason: ocv1.ClusterObjectDeploymentReasonRevisionObjectCollision, Message: "blocking detail",
					ObservedGeneration: 2,
				},
			},
			want: expected{
				reason:             ocv1.ReasonRetrying,
				status:             metav1.ConditionTrue,
				observedGeneration: 5,
				message:            "Helm migration Protecting: blocking detail",
			},
		},
		{
			name: "precondition",
			input: input{
				protection:           ocv1.CollisionProtectionPrevent,
				extensionGeneration:  5,
				deploymentGeneration: 2,
				progressing: metav1.Condition{
					Type:   ocv1.ClusterObjectDeploymentTypeProgressing,
					Status: metav1.ConditionFalse,
					Reason: ocv1.ClusterObjectDeploymentReasonRevisionPreconditionsNotMet, Message: "blocking detail",
					ObservedGeneration: 2,
				},
			},
			want: expected{
				reason:             ocv1.ReasonRetrying,
				status:             metav1.ConditionTrue,
				observedGeneration: 5,
				message:            "Helm migration Protecting: blocking detail",
			},
		},
		{
			name: "deadline",
			input: input{
				protection:           ocv1.CollisionProtectionPrevent,
				extensionGeneration:  5,
				deploymentGeneration: 2,
				progressing: metav1.Condition{
					Type:   ocv1.ClusterObjectDeploymentTypeProgressing,
					Status: metav1.ConditionFalse,
					Reason: ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, Message: "blocking detail",
					ObservedGeneration: 2,
				},
			},
			want: expected{
				reason:             ocv1.ReasonProgressDeadlineExceeded,
				status:             metav1.ConditionFalse,
				observedGeneration: 5,
				message:            "Helm migration Protecting: blocking detail",
			},
		},
		{
			name: "invalid",
			input: input{
				protection:           ocv1.CollisionProtectionPrevent,
				extensionGeneration:  5,
				deploymentGeneration: 2,
				progressing: metav1.Condition{
					Type:   ocv1.ClusterObjectDeploymentTypeProgressing,
					Status: metav1.ConditionFalse,
					Reason: ocv1.ClusterObjectDeploymentReasonInvalidSpecification, Message: "blocking detail",
					ObservedGeneration: 2,
				},
			},
			want: expected{
				reason:             ocv1.ReasonBlocked,
				status:             metav1.ConditionFalse,
				observedGeneration: 5,
				message:            "Helm migration Protecting: blocking detail",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given a failure observed at the COD's current generation.
			ext, cod := migrationStatusFixture(tc.input.protection)
			ext.Generation = tc.input.extensionGeneration
			cod.Generation = tc.input.deploymentGeneration
			cod.Status.Conditions = []metav1.Condition{tc.input.progressing}

			// When migration status is reflected onto the CE.
			setMigrationStatus(ext, cod)

			// Then the CE exposes the appropriate retry or terminal reason and blocking detail.
			progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
			require.NotNil(t, progress)
			require.Equal(t, tc.want.reason, progress.Reason)
			require.Equal(t, tc.want.status, progress.Status)
			require.Equal(t, tc.want.observedGeneration, progress.ObservedGeneration)
			require.Equal(t, tc.want.message, progress.Message)
		})
	}
}

func TestMigrationStatusIgnoresStaleProgress(t *testing.T) {
	type input struct {
		protection           ocv1.CollisionProtection
		extensionGeneration  int64
		deploymentGeneration int64
		progressing          metav1.Condition
	}
	type expected struct {
		reason string
		status metav1.ConditionStatus
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "old completion",
			input: input{
				protection:           ocv1.CollisionProtectionPrevent,
				extensionGeneration:  5,
				deploymentGeneration: 2,
				progressing: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeProgressing,
					Status:             metav1.ConditionFalse,
					Reason:             ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
					ObservedGeneration: 1,
				},
			},
			want: expected{
				reason: ocv1.ReasonRollingOut,
				status: metav1.ConditionTrue,
			},
		},
		{
			name: "old deadline failure",
			input: input{
				protection:           ocv1.CollisionProtectionPrevent,
				extensionGeneration:  5,
				deploymentGeneration: 2,
				progressing: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeProgressing,
					Status:             metav1.ConditionFalse,
					Reason:             ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded,
					ObservedGeneration: 1,
				},
			},
			want: expected{
				reason: ocv1.ReasonRollingOut,
				status: metav1.ConditionTrue,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given progress observed before the current COD generation.
			ext, cod := migrationStatusFixture(tc.input.protection)
			ext.Generation = tc.input.extensionGeneration
			cod.Generation = tc.input.deploymentGeneration
			cod.Status.Conditions = []metav1.Condition{tc.input.progressing}

			// When migration status is reflected onto the CE.
			setMigrationStatus(ext, cod)

			// Then stale completion or failure does not replace the current rollout progress.
			progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
			require.NotNil(t, progress)
			require.Equal(t, tc.want.reason, progress.Reason)
			require.Equal(t, tc.want.status, progress.Status)
		})
	}
}

func TestMigrationTransitionReportsProgressInsteadOfSuccess(t *testing.T) {
	input := struct {
		protection           ocv1.CollisionProtection
		deploymentGeneration int64
		progressing          metav1.Condition
	}{
		protection:           ocv1.CollisionProtectionPrevent,
		deploymentGeneration: 2,
		progressing: metav1.Condition{
			Type:               ocv1.ClusterObjectDeploymentTypeProgressing,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
			ObservedGeneration: 2,
		},
	}
	want := struct {
		status metav1.ConditionStatus
		reason string
	}{
		status: metav1.ConditionTrue,
		reason: ocv1.ReasonRollingOut,
	}

	// Given protection has just started but the COD still reports adoption completion.
	ext, cod := migrationStatusFixture(input.protection)
	cod.Generation = input.deploymentGeneration
	cod.Status.Conditions = []metav1.Condition{input.progressing}

	// When migration status is reflected before the COD controller observes the new protection.
	setMigrationStatus(ext, cod)

	// Then the CE reports ongoing migration rather than overall success between stages.
	progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, progress)
	require.Equal(t, want.reason, progress.Reason)
	require.Equal(t, want.status, progress.Status)
}

// migrationStatusFixture returns fresh, unstored CE/COD data for bundle 1.0.0.
// The generation-2 COD is staged for the CE and has no rollout conditions.
func migrationStatusFixture(protection ocv1.CollisionProtection) (*ocv1.ClusterExtension, *ocv1.ClusterObjectDeployment) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "migration", UID: "current-extension", Generation: 5}}
	cod := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: ext.Name, UID: "deployment", Generation: 2,
			OwnerReferences: []metav1.OwnerReference{{Kind: ocv1.ClusterExtensionKind, APIVersion: ocv1.GroupVersion.String(), Name: ext.Name, UID: ext.UID}},
		},
		Spec: ocv1.ClusterObjectDeploymentSpec{Template: ocv1.ClusterObjectDeploymentTemplate{
			Metadata: ocv1.ClusterObjectDeploymentTemplateMetadata{Annotations: map[string]string{
				labels.BundleNameKey: "stored.v1.0.0", labels.BundleVersionKey: "1.0.0", labels.BundleReferenceKey: "image:stored", labels.BundleReleaseKey: "",
			}},
			Spec: ocv1.ClusterObjectDeploymentTemplateSpec{
				ClusterObjectSetContent: ocv1.ClusterObjectSetContent{CollisionProtection: protection},
			},
		}},
	}
	return ext, cod
}
