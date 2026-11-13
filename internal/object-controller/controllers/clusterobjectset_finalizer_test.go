//go:build !standard

package controllers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestClusterObjectSetEnsureFinalizerPreservesStatusForUpdate(t *testing.T) {
	cl, cos := newFinalizerTestObject(t)
	wantStatus := cos.Status.DeepCopy()

	reconciler := &ClusterObjectSetReconciler{Client: cl}
	require.NoError(t, reconciler.ensureFinalizer(t.Context(), cos, clusterObjectSetTeardownFinalizer))
	require.NoError(t, cl.Status().Update(t.Context(), cos))

	stored := &ocv1.ClusterObjectSet{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(cos), stored))
	require.True(t, equality.Semantic.DeepEqual(*wantStatus, stored.Status),
		"computed status must survive finalizer addition and the subsequent update")
}

func newFinalizerTestObject(t *testing.T) (client.Client, *ocv1.ClusterObjectSet) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))
	cos := newTestClusterObjectSetInternal(t, "test-ext-1")
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		WithObjects(cos).Build()
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(cos), cos))

	// Phase resolution computes this status before the finalizer is added. It
	// has not been written to the API server when the metadata patch runs.
	cos.Status.ObservedPhases = []ocv1.ObservedPhase{{
		Name:               "everything",
		Digest:             "sha256:e159e3f2c46b65df156d02407c44936c0fd7349149a89dadf190d27c67019edc",
		Status:             ocv1.PhaseStatusUnknown,
		LastTransitionTime: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		Message:            "phase has not been evaluated",
	}}
	return cl, cos
}
