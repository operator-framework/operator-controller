package controllers_test

import (
	"context"
	"errors"
	"testing"

	orbv1alpha1 "github.com/joelanford/orb-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

func orbTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, orbv1alpha1.AddToScheme(scheme))
	return scheme
}

// cosRevision builds a ClusterObjectSet revision for the given group.
func cosRevision(name, group string, revision uint32, lifecycle orbv1alpha1.LifecycleState, completed bool, annotations map[string]string) *orbv1alpha1.ClusterObjectSet {
	cos := &orbv1alpha1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Spec: orbv1alpha1.ClusterObjectSetSpec{
			Group:          group,
			Revision:       revision,
			LifecycleState: lifecycle,
		},
	}
	if completed {
		now := metav1.Now()
		cos.Status.CompletedAt = &now
	}
	return cos
}

func bundleAnnotations(name, version, pkg, ref string) map[string]string {
	return map[string]string{
		labels.BundleNameKey:      name,
		labels.BundleVersionKey:   version,
		labels.PackageNameKey:     pkg,
		labels.BundleReferenceKey: ref,
	}
}

func newOrbGetter(t *testing.T, objs ...client.Object) *controllers.OrbOperatorRevisionStatesGetter {
	t.Helper()
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbTestScheme(t)).
		WithObjects(objs...).
		WithIndex(&orbv1alpha1.ClusterObjectSet{}, "spec.group", func(o client.Object) []string {
			return []string{o.(*orbv1alpha1.ClusterObjectSet).Spec.Group}
		}).
		Build()
	return &controllers.OrbOperatorRevisionStatesGetter{Reader: fakeClient}
}

func TestOrbGetRevisionStates_SingleCompleted(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	getter := newOrbGetter(t,
		cosRevision("argocd-1", "argocd", 1, orbv1alpha1.LifecycleStateActive, true,
			bundleAnnotations("argocd-operator.v0.13.0", "0.13.0", "argocd-operator", "quay.io/argocd@sha256:abc")),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.NotNil(t, rs.Installed)
	assert.Empty(t, rs.RollingOut)
	assert.Equal(t, "argocd-1", rs.Installed.RevisionName)
	assert.Equal(t, "argocd-operator.v0.13.0", rs.Installed.Name)
	assert.Equal(t, "0.13.0", rs.Installed.Version)
	assert.Equal(t, "argocd-operator", rs.Installed.Package)
	assert.Equal(t, "quay.io/argocd@sha256:abc", rs.Installed.Image)
	assert.Nil(t, rs.Installed.Release)
}

func TestOrbGetRevisionStates_SingleRollingOut(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	getter := newOrbGetter(t,
		cosRevision("argocd-1", "argocd", 1, orbv1alpha1.LifecycleStateActive, false,
			bundleAnnotations("argocd-operator.v0.13.0", "0.13.0", "argocd-operator", "ref")),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	assert.Nil(t, rs.Installed)
	require.Len(t, rs.RollingOut, 1)
	assert.Equal(t, "argocd-1", rs.RollingOut[0].RevisionName)
}

func TestOrbGetRevisionStates_MixedInstalledAndRollingOut(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	// Seed out of order to also exercise the ascending sort.
	getter := newOrbGetter(t,
		cosRevision("argocd-2", "argocd", 2, orbv1alpha1.LifecycleStateActive, false,
			bundleAnnotations("argocd-operator.v0.14.0", "0.14.0", "argocd-operator", "ref2")),
		cosRevision("argocd-1", "argocd", 1, orbv1alpha1.LifecycleStateActive, true,
			bundleAnnotations("argocd-operator.v0.13.0", "0.13.0", "argocd-operator", "ref1")),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.NotNil(t, rs.Installed)
	assert.Equal(t, "argocd-1", rs.Installed.RevisionName)
	require.Len(t, rs.RollingOut, 1)
	assert.Equal(t, "argocd-2", rs.RollingOut[0].RevisionName)
}

func TestOrbGetRevisionStates_MultipleCompletedHighestWins(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	getter := newOrbGetter(t,
		cosRevision("argocd-1", "argocd", 1, orbv1alpha1.LifecycleStateActive, true,
			bundleAnnotations("argocd-operator.v0.13.0", "0.13.0", "argocd-operator", "ref1")),
		cosRevision("argocd-2", "argocd", 2, orbv1alpha1.LifecycleStateActive, true,
			bundleAnnotations("argocd-operator.v0.14.0", "0.14.0", "argocd-operator", "ref2")),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.NotNil(t, rs.Installed)
	assert.Equal(t, "argocd-2", rs.Installed.RevisionName)
	assert.Equal(t, "0.14.0", rs.Installed.Version)
	assert.Empty(t, rs.RollingOut)
}

func TestOrbGetRevisionStates_SkipsArchived(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	getter := newOrbGetter(t,
		cosRevision("argocd-1", "argocd", 1, orbv1alpha1.LifecycleStateArchived, true,
			bundleAnnotations("argocd-operator.v0.13.0", "0.13.0", "argocd-operator", "ref1")),
		cosRevision("argocd-2", "argocd", 2, orbv1alpha1.LifecycleStateActive, true,
			bundleAnnotations("argocd-operator.v0.14.0", "0.14.0", "argocd-operator", "ref2")),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.NotNil(t, rs.Installed)
	assert.Equal(t, "argocd-2", rs.Installed.RevisionName)
	assert.Empty(t, rs.RollingOut)
}

func TestOrbGetRevisionStates_ReleaseOnlyWhenPresent(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}

	withRelease := bundleAnnotations("argocd-operator.v0.13.0", "0.13.0", "argocd-operator", "ref")
	withRelease[labels.BundleReleaseKey] = "3"

	getter := newOrbGetter(t,
		cosRevision("argocd-1", "argocd", 1, orbv1alpha1.LifecycleStateActive, true, withRelease),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.NotNil(t, rs.Installed)
	require.NotNil(t, rs.Installed.Release)
	assert.Equal(t, "3", *rs.Installed.Release)
}

func TestOrbGetRevisionStates_FiltersByGroup(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	getter := newOrbGetter(t,
		cosRevision("argocd-1", "argocd", 1, orbv1alpha1.LifecycleStateActive, true,
			bundleAnnotations("argocd-operator.v0.13.0", "0.13.0", "argocd-operator", "ref")),
		cosRevision("other-1", "other", 1, orbv1alpha1.LifecycleStateActive, true,
			bundleAnnotations("other.v1", "1.0.0", "other", "ref")),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.NotNil(t, rs.Installed)
	assert.Equal(t, "argocd-1", rs.Installed.RevisionName)
	assert.Empty(t, rs.RollingOut)
}

func TestOrbGetRevisionStates_NoRevisions(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	getter := newOrbGetter(t)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.NotNil(t, rs)
	assert.Nil(t, rs.Installed)
	assert.Empty(t, rs.RollingOut)
}

func TestOrbGetRevisionStates_ListError(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbTestScheme(t)).
		WithIndex(&orbv1alpha1.ClusterObjectSet{}, "spec.group", func(o client.Object) []string {
			return []string{o.(*orbv1alpha1.ClusterObjectSet).Spec.Group}
		}).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return errors.New("boom")
			},
		}).
		Build()
	getter := &controllers.OrbOperatorRevisionStatesGetter{Reader: fakeClient}

	_, err := getter.GetRevisionStates(context.Background(), ext)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing revisions")
}

// rollingCOSWithPhase builds a rolling-out (not completed) revision of group
// "argocd" carrying a single observed phase.
func rollingCOSWithPhase(phase orbv1alpha1.ObservedPhase) *orbv1alpha1.ClusterObjectSet {
	cos := cosRevision("argocd-1", "argocd", 1, orbv1alpha1.LifecycleStateActive, false,
		bundleAnnotations("pkg.v1", "1.0.0", "pkg", "ref"))
	cos.Status.ObservedPhases = []orbv1alpha1.ObservedPhase{phase}
	return cos
}

// orbCOD builds a ClusterObjectDeployment with a single Progressing condition.
func orbCOD(name, reason string, status metav1.ConditionStatus) *orbv1alpha1.ClusterObjectDeployment {
	return &orbv1alpha1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: orbv1alpha1.ClusterObjectDeploymentStatus{
			Conditions: []metav1.Condition{{
				Type:               orbv1alpha1.ConditionTypeProgressing,
				Status:             status,
				Reason:             reason,
				Message:            reason,
				LastTransitionTime: metav1.Now(),
			}},
		},
	}
}

func requireProgressing(t *testing.T, rm *controllers.RevisionMetadata) *metav1.Condition {
	t.Helper()
	require.NotNil(t, rm)
	c := apimeta.FindStatusCondition(rm.Conditions, ocv1.ClusterObjectSetTypeProgressing)
	require.NotNil(t, c, "expected a Progressing condition")
	return c
}

func TestOrbGetRevisionStates_CompletedReportsSucceeded(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	getter := newOrbGetter(t,
		cosRevision("argocd-1", "argocd", 1, orbv1alpha1.LifecycleStateActive, true,
			bundleAnnotations("argocd-operator.v0.13.0", "0.13.0", "argocd-operator", "ref")),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	cond := requireProgressing(t, rs.Installed)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, ocv1.ReasonSucceeded, cond.Reason)
}

func TestOrbGetRevisionStates_HealthyRolloutReportsRollingOut(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	// All objects synced, assertions/probes still pending: WaitingForAssertions.
	getter := newOrbGetter(t,
		rollingCOSWithPhase(orbv1alpha1.ObservedPhase{
			Name:         "deploy",
			Status:       orbv1alpha1.PhaseStatusWaitingForAssertions,
			ObjectCounts: orbv1alpha1.ObjectCounts{Total: 1, Synced: 1, Available: 0},
		}),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.Len(t, rs.RollingOut, 1)
	cond := requireProgressing(t, rs.RollingOut[0])
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, ocv1.ReasonRollingOut, cond.Reason)
}

func TestOrbGetRevisionStates_InvalidPhaseReportsRetrying(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	// The recover.feature collision: orb's preflight dry-run rejects the
	// immutable selector, marking the phase Invalid with synced<total.
	getter := newOrbGetter(t,
		rollingCOSWithPhase(orbv1alpha1.ObservedPhase{
			Name:         "deploy",
			Status:       orbv1alpha1.PhaseStatusInvalid,
			ObjectCounts: orbv1alpha1.ObjectCounts{Total: 1, Synced: 0},
			ObjectDetails: []orbv1alpha1.ObjectStatus{{
				Kind: "Deployment", Name: "test-operator", Messages: []string{"spec.selector: field is immutable"},
			}},
		}),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.Len(t, rs.RollingOut, 1)
	cond := requireProgressing(t, rs.RollingOut[0])
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, ocv1.ClusterObjectSetReasonRetrying, cond.Reason)
	assert.Contains(t, cond.Message, "immutable")
}

func TestOrbGetRevisionStates_UnsyncedObjectReportsRetrying(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	// Not Invalid, but an object cannot be synced and carries a failure message.
	getter := newOrbGetter(t,
		rollingCOSWithPhase(orbv1alpha1.ObservedPhase{
			Name:         "deploy",
			Status:       orbv1alpha1.PhaseStatusReconciling,
			ObjectCounts: orbv1alpha1.ObjectCounts{Total: 2, Synced: 1},
			ObjectDetails: []orbv1alpha1.ObjectStatus{{
				Kind: "Deployment", Name: "x", Messages: []string{"apply error"},
			}},
		}),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.Len(t, rs.RollingOut, 1)
	cond := requireProgressing(t, rs.RollingOut[0])
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, ocv1.ClusterObjectSetReasonRetrying, cond.Reason)
}

func TestOrbGetRevisionStates_ReconcilingWithoutErrorReportsRollingOut(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	// Mid-apply: synced<total but no object-level failure details -> healthy.
	getter := newOrbGetter(t,
		rollingCOSWithPhase(orbv1alpha1.ObservedPhase{
			Name:         "deploy",
			Status:       orbv1alpha1.PhaseStatusReconciling,
			ObjectCounts: orbv1alpha1.ObjectCounts{Total: 2, Synced: 1},
		}),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.Len(t, rs.RollingOut, 1)
	cond := requireProgressing(t, rs.RollingOut[0])
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, ocv1.ReasonRollingOut, cond.Reason)
}

func TestOrbGetRevisionStates_ProgressDeadlineExceededReportsFalse(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	// Deadline wins over a blocked phase.
	getter := newOrbGetter(t,
		rollingCOSWithPhase(orbv1alpha1.ObservedPhase{
			Name:         "deploy",
			Status:       orbv1alpha1.PhaseStatusInvalid,
			ObjectCounts: orbv1alpha1.ObjectCounts{Total: 1, Synced: 0},
			ObjectDetails: []orbv1alpha1.ObjectStatus{{
				Kind: "Deployment", Name: "x", Messages: []string{"boom"},
			}},
		}),
		orbCOD("argocd", orbv1alpha1.ReasonProgressDeadlineExceeded, metav1.ConditionFalse),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.Len(t, rs.RollingOut, 1)
	cond := requireProgressing(t, rs.RollingOut[0])
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, ocv1.ReasonProgressDeadlineExceeded, cond.Reason)
}

func TestOrbGetRevisionStates_CODErrorReasonReportsRetrying(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	// No blocked phase, but the COD reports a reconcile error.
	getter := newOrbGetter(t,
		rollingCOSWithPhase(orbv1alpha1.ObservedPhase{
			Name:         "deploy",
			Status:       orbv1alpha1.PhaseStatusReconciling,
			ObjectCounts: orbv1alpha1.ObjectCounts{Total: 1, Synced: 1},
		}),
		orbCOD("argocd", orbv1alpha1.ReasonReconcileError, metav1.ConditionTrue),
	)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.Len(t, rs.RollingOut, 1)
	cond := requireProgressing(t, rs.RollingOut[0])
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, ocv1.ClusterObjectSetReasonRetrying, cond.Reason)
}

func TestOrbGetRevisionStates_AvailablePassesThrough(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd"}}
	cos := rollingCOSWithPhase(orbv1alpha1.ObservedPhase{
		Name:         "deploy",
		Status:       orbv1alpha1.PhaseStatusReconciling,
		ObjectCounts: orbv1alpha1.ObjectCounts{Total: 1, Synced: 1},
	})
	cos.Status.Conditions = []metav1.Condition{{
		Type:               orbv1alpha1.ConditionTypeAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             orbv1alpha1.ReasonUnavailable,
		Message:            "phases not yet complete",
		LastTransitionTime: metav1.Now(),
	}}
	getter := newOrbGetter(t, cos)

	rs, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)
	require.Len(t, rs.RollingOut, 1)
	avail := apimeta.FindStatusCondition(rs.RollingOut[0].Conditions, ocv1.ClusterObjectSetTypeAvailable)
	require.NotNil(t, avail)
	assert.Equal(t, metav1.ConditionFalse, avail.Status)
	assert.Equal(t, orbv1alpha1.ReasonUnavailable, avail.Reason)
}
