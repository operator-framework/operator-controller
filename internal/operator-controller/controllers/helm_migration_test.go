package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

type migrateFunc func(context.Context, *ocv1.ClusterExtension, map[string]string) (*applier.PendingMigration, error)

func (f migrateFunc) Migrate(ctx context.Context, ext *ocv1.ClusterExtension, labels map[string]string) (*applier.PendingMigration, error) {
	return f(ctx, ext, labels)
}

func TestMigrateStorageGatesFollowingSteps(t *testing.T) {
	_, cod := migrationStatusFixture(ocv1.CollisionProtectionPrevent)
	migrationErr := errors.New("API unavailable")
	testCases := []struct {
		name          string
		pending       *applier.PendingMigration
		migrationErr  error
		wantFollowing bool
		wantRequeue   time.Duration
		wantErr       error
	}{
		{name: "migration pending", pending: &applier.PendingMigration{Deployment: cod}, wantRequeue: 10 * time.Second},
		{name: "migration failed", migrationErr: migrationErr, wantErr: migrationErr},
		{name: "migration inactive", wantFollowing: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ext, _ := migrationStatusFixture(ocv1.CollisionProtectionPrevent)
			m := migrateFunc(func(context.Context, *ocv1.ClusterExtension, map[string]string) (*applier.PendingMigration, error) {
				return tc.pending, tc.migrationErr
			})
			var following bool
			steps := ReconcileSteps{
				MigrateStorage(m),
				func(context.Context, *reconcileState, *ocv1.ClusterExtension) (*ctrl.Result, error) {
					following = true
					return nil, nil
				},
			}

			result, err := steps.Reconcile(t.Context(), ext)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantFollowing, following)
			require.Equal(t, tc.wantRequeue, result.RequeueAfter)
		})
	}
}

func TestMigrateStorageReportsMigrationError(t *testing.T) {
	input := errors.New("listing Helm release Secrets: API unavailable")
	ext, _ := migrationStatusFixture(ocv1.CollisionProtectionNone)
	want := struct {
		err       error
		result    *ctrl.Result
		condition metav1.Condition
	}{
		err:    input,
		result: nil,
		condition: metav1.Condition{
			Type:               ocv1.ClusterExtensionTypeProgressing,
			Status:             metav1.ConditionTrue,
			Reason:             ocv1.ClusterExtensionReasonRetrying,
			Message:            "migrating storage: listing Helm release Secrets: API unavailable",
			ObservedGeneration: ext.Generation,
		},
	}
	m := migrateFunc(func(context.Context, *ocv1.ClusterExtension, map[string]string) (*applier.PendingMigration, error) {
		return nil, input
	})

	result, err := MigrateStorage(m)(t.Context(), &reconcileState{}, ext)

	require.ErrorIs(t, err, want.err)
	require.Equal(t, want.result, result)
	progress := apimeta.FindStatusCondition(ext.Status.Conditions, want.condition.Type)
	require.NotNil(t, progress)
	require.Equal(t, want.condition.Status, progress.Status)
	require.Equal(t, want.condition.Reason, progress.Reason)
	require.Equal(t, want.condition.ObservedGeneration, progress.ObservedGeneration)
	require.Equal(t, want.condition.Message, progress.Message)
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
			installed := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.ClusterExtensionTypeInstalled)
			require.NotNil(t, installed)
			require.Equal(t, tc.want.installedStatus, installed.Status)
			require.Contains(t, installed.Message, tc.want.installedImage)
			require.Equal(t, tc.want.available, apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.ClusterExtensionTypeAvailable))
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
					Type:               ocv1.ClusterExtensionTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ClusterExtensionReasonProbesSucceeded,
					Message:            "Objects are available and pass all probes.",
					ObservedGeneration: 5,
				},
				progressingReason: ocv1.ClusterExtensionReasonRollingOut,
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
					Type:               ocv1.ClusterExtensionTypeAvailable,
					Status:             metav1.ConditionFalse,
					Reason:             "ObservedHealth",
					Message:            "live probes",
					ObservedGeneration: 5,
				},
				progressingReason: ocv1.ClusterExtensionReasonRollingOut,
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
			progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.ClusterExtensionTypeProgressing)
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
				reason:             ocv1.ClusterExtensionReasonRetrying,
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
				reason:             ocv1.ClusterExtensionReasonRetrying,
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
				reason:             ocv1.ClusterExtensionReasonProgressDeadlineExceeded,
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
				reason:             ocv1.ClusterExtensionReasonBlocked,
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
			progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.ClusterExtensionTypeProgressing)
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
				reason: ocv1.ClusterExtensionReasonRollingOut,
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
				reason: ocv1.ClusterExtensionReasonRollingOut,
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
			progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.ClusterExtensionTypeProgressing)
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
		reason: ocv1.ClusterExtensionReasonRollingOut,
	}

	// Given protection has just started but the COD still reports adoption completion.
	ext, cod := migrationStatusFixture(input.protection)
	cod.Generation = input.deploymentGeneration
	cod.Status.Conditions = []metav1.Condition{input.progressing}

	// When migration status is reflected before the COD controller observes the new protection.
	setMigrationStatus(ext, cod)

	// Then the CE reports ongoing migration rather than overall success between stages.
	progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.ClusterExtensionTypeProgressing)
	require.NotNil(t, progress)
	require.Equal(t, want.reason, progress.Reason)
	require.Equal(t, want.status, progress.Status)
}

// migrationStatusFixture returns fresh, unstored CE/COD data for bundle 1.0.0.
// The generation-2 COD is owned by the CE and has no rollout conditions.
func migrationStatusFixture(protection ocv1.CollisionProtection) (*ocv1.ClusterExtension, *ocv1.ClusterObjectDeployment) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "migration", UID: "current-extension", Generation: 5}}
	cod := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: ext.Name, UID: "deployment", Generation: 2,
			Labels:          map[string]string{labels.ManagedByKey: labels.ManagedByOperatorController},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind))},
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
