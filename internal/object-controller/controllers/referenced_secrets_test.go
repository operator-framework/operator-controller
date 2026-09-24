package controllers_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clocktesting "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
)

func TestReferencedSecretsReadOncePerReconcile(t *testing.T) {
	ctx := t.Context()
	mockCtrl := gomock.NewController(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "content", Namespace: "source-a"},
		Immutable:  ptr.To(true), Data: map[string][]byte{},
	}
	cos := newRefTestCOS("packed", ocv1.ObjectSourceRef{})
	cos.Spec.Phases = nil
	for phaseIndex := range 2 {
		phase := ocv1.ClusterObjectSetPhase{Name: fmt.Sprintf("phase-%d", phaseIndex)}
		for objectIndex := range 25 {
			name := fmt.Sprintf("cm-%d-%d", phaseIndex, objectIndex)
			secret.Data[name] = []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q,"namespace":"target"}}`, name))
			phase.Objects = append(phase.Objects, ocv1.ClusterObjectSetObject{Ref: ocv1.ObjectSourceRef{Name: secret.Name, Namespace: secret.Namespace, Key: name}})
		}
		cos.Spec.Phases = append(cos.Spec.Phases, phase)
	}
	otherSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secret.Name, Namespace: "source-b"},
		Immutable:  ptr.To(true),
		Data:       map[string][]byte{"other": []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"other","namespace":"target"}}`)},
	}
	cos.Spec.Phases[1].Objects = append(cos.Spec.Phases[1].Objects, ocv1.ClusterObjectSetObject{Ref: ocv1.ObjectSourceRef{Name: otherSecret.Name, Namespace: otherSecret.Namespace, Key: "other"}})
	reads := map[client.ObjectKey]int{}
	cl := fake.NewClientBuilder().WithScheme(newSchemeWithCoreV1(t)).WithObjects(secret, otherSecret, cos).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.Secret); ok {
					reads[key]++
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).Build()
	engine := newMockRevisionEngineWithReconcile(mockCtrl,
		func(context.Context, machinerytypes.Revision, ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
			return newMockRevisionResult(mockCtrl, revisionResultConfig{inTransition: true}), nil
		}, nil)
	reconciler := &controllers.ClusterObjectSetReconciler{
		Client: cl, TrackingCache: newMockTrackingCache(mockCtrl, cl, nil),
		RevisionEngineFactory: newMockRevisionEngineFactoryWithEngine(mockCtrl, engine, nil),
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}
	reconcile := func(wantReads int, wantReason string) {
		t.Helper()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cos)})
		require.NoError(t, err)
		require.Equal(t, wantReads, reads[client.ObjectKeyFromObject(secret)])
		require.Equal(t, wantReads, reads[client.ObjectKeyFromObject(otherSecret)])
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cos), cos))
		condition := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeProgressing)
		require.NotNil(t, condition)
		require.Equal(t, wantReason, condition.Reason)
	}
	reconcile(1, ocv1.ReasonRollingOut)
	reconcile(2, ocv1.ReasonRollingOut)

	// Reusing a fetched Secret must not hide a replacement on the next reconcile.
	require.NoError(t, cl.Delete(ctx, secret))
	changed := secret.DeepCopy()
	changed.ResourceVersion = ""
	changed.Data["cm-0-0"] = []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm-0-0","namespace":"target"},"data":{"value":"changed"}}`)
	require.NoError(t, cl.Create(ctx, changed))
	reconcile(3, ocv1.ClusterObjectSetReasonBlocked)
	require.Contains(t, meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeProgressing).Message, "resolved content of 1 phase(s) has changed")

	require.NoError(t, cl.Delete(ctx, changed))
	secret.ResourceVersion = ""
	require.NoError(t, cl.Create(ctx, secret))
	reconcile(4, ocv1.ReasonRollingOut)
}

func TestReferencedSecretReadFailureRetries(t *testing.T) {
	ctx := t.Context()
	mockCtrl := gomock.NewController(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "content", Namespace: "source"},
		Immutable:  ptr.To(true),
		Data:       map[string][]byte{"object": []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm","namespace":"target"}}`)},
	}
	cos := newRefTestCOS("retry", ocv1.ObjectSourceRef{Name: secret.Name, Namespace: secret.Namespace, Key: "object"})
	failReads := true
	cl := fake.NewClientBuilder().WithScheme(newSchemeWithCoreV1(t)).WithObjects(secret, cos).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.Secret); ok && failReads {
					return apierrors.NewServiceUnavailable("temporary API outage")
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).Build()
	engine := newMockRevisionEngineWithReconcile(mockCtrl,
		func(context.Context, machinerytypes.Revision, ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
			return newMockRevisionResult(mockCtrl, revisionResultConfig{inTransition: true}), nil
		}, nil)
	reconciler := &controllers.ClusterObjectSetReconciler{
		Client: cl, TrackingCache: newMockTrackingCache(mockCtrl, cl, nil),
		RevisionEngineFactory: newMockRevisionEngineFactoryWithEngine(mockCtrl, engine, nil),
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cos)}
	for range 2 {
		_, err := reconciler.Reconcile(ctx, req)
		require.ErrorContains(t, err, "temporary API outage")
		require.NoError(t, cl.Get(ctx, req.NamespacedName, cos))
		condition := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeProgressing)
		require.NotNil(t, condition)
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, ocv1.ReasonRetrying, condition.Reason)
	}
	failReads = false
	_, err := reconciler.Reconcile(ctx, req)
	require.NoError(t, err)
	require.NoError(t, cl.Get(ctx, req.NamespacedName, cos))
	require.Equal(t, ocv1.ReasonRollingOut, meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeProgressing).Reason)
}
