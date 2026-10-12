package controllers_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

func newSchemeWithCoreV1(t *testing.T) *apimachineryruntime.Scheme {
	t.Helper()
	sch := apimachineryruntime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(sch))
	require.NoError(t, corev1.AddToScheme(sch))
	return sch
}

func TestResolveObjectRef_PlainJSON(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	testScheme := newSchemeWithCoreV1(t)

	cmObj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "test-cm",
			"namespace": "default",
		},
	}
	cmData, err := json.Marshal(cmObj)
	require.NoError(t, err)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "olmv1-system",
		},
		Immutable: ptr.To(true),
		Data: map[string][]byte{
			"my-key": cmData,
		},
	}

	cos := newRefTestCOS("ref-plain-1", ocv1.ObjectSourceRef{
		Name:      "test-secret",
		Namespace: "olmv1-system",
		Key:       "my-key",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(secret, cos).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		Build()

	mockEngine := newMockRevisionEngineWithReconcile(mockCtrl,
		func(_ context.Context, _ machinerytypes.Revision, _ ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
			return newMockRevisionResult(mockCtrl, revisionResultConfig{}), nil
		}, nil,
	)
	reconciler := &controllers.ClusterObjectSetReconciler{
		Client:                fakeClient,
		RevisionEngineFactory: newMockRevisionEngineFactoryWithEngine(mockCtrl, mockEngine, nil),
		TrackingCache:         newMockTrackingCache(mockCtrl, fakeClient, nil),
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}

	_, err = reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cos.Name},
	})
	require.NoError(t, err)
}

func TestResolveObjectRef_GzipCompressed(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	testScheme := newSchemeWithCoreV1(t)

	cmObj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "test-cm",
			"namespace": "default",
		},
	}
	cmData, err := json.Marshal(cmObj)
	require.NoError(t, err)

	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	require.NoError(t, err)
	_, err = w.Write(cmData)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret-gz",
			Namespace: "olmv1-system",
		},
		Immutable: ptr.To(true),
		Data: map[string][]byte{
			"my-key": buf.Bytes(),
		},
	}

	cos := newRefTestCOS("ref-gzip-1", ocv1.ObjectSourceRef{
		Name:      "test-secret-gz",
		Namespace: "olmv1-system",
		Key:       "my-key",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(secret, cos).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		Build()

	mockEngine := newMockRevisionEngineWithReconcile(mockCtrl,
		func(_ context.Context, _ machinerytypes.Revision, _ ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
			return newMockRevisionResult(mockCtrl, revisionResultConfig{}), nil
		}, nil,
	)
	reconciler := &controllers.ClusterObjectSetReconciler{
		Client:                fakeClient,
		RevisionEngineFactory: newMockRevisionEngineFactoryWithEngine(mockCtrl, mockEngine, nil),
		TrackingCache:         newMockTrackingCache(mockCtrl, fakeClient, nil),
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}

	_, err = reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cos.Name},
	})
	require.NoError(t, err)
}

func TestResolveObjectRef_SecretNotFound(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	testScheme := newSchemeWithCoreV1(t)

	cos := newRefTestCOS("ref-notfound-1", ocv1.ObjectSourceRef{
		Name:      "nonexistent-secret",
		Namespace: "olmv1-system",
		Key:       "my-key",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(cos).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		Build()

	reconciler := &controllers.ClusterObjectSetReconciler{
		Client:                fakeClient,
		RevisionEngineFactory: newMockRevisionEngineFactoryWithEngine(mockCtrl, newNoopMockRevisionEngine(mockCtrl), nil),
		TrackingCache:         newMockTrackingCache(mockCtrl, fakeClient, nil),
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cos.Name},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving ref")
}

func TestResolveObjectRef_KeyNotFound(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	testScheme := newSchemeWithCoreV1(t)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret-nokey",
			Namespace: "olmv1-system",
		},
		Immutable: ptr.To(true),
		Data: map[string][]byte{
			"other-key": []byte("{}"),
		},
	}

	cos := newRefTestCOS("ref-nokey-1", ocv1.ObjectSourceRef{
		Name:      "test-secret-nokey",
		Namespace: "olmv1-system",
		Key:       "missing-key",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(secret, cos).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		Build()

	reconciler := &controllers.ClusterObjectSetReconciler{
		Client:                fakeClient,
		RevisionEngineFactory: newMockRevisionEngineFactoryWithEngine(mockCtrl, newNoopMockRevisionEngine(mockCtrl), nil),
		TrackingCache:         newMockTrackingCache(mockCtrl, fakeClient, nil),
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}

	// A missing key in an (immutable) referenced Secret is invalid configuration that will
	// not resolve on retry: no error is returned, and Ready reports ValidationFailure.
	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cos.Name},
	})
	require.NoError(t, err)

	updated := &ocv1.ClusterObjectSet{}
	require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKey{Name: cos.Name}, updated))
	cond := meta.FindStatusCondition(updated.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, ocv1.ClusterObjectSetReasonValidationFailure, cond.Reason)
	assert.Contains(t, cond.Message, "key")
}

func TestResolveObjectRef_InvalidJSON(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	testScheme := newSchemeWithCoreV1(t)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret-invalid",
			Namespace: "olmv1-system",
		},
		Immutable: ptr.To(true),
		Data: map[string][]byte{
			"my-key": []byte("not-valid-json"),
		},
	}

	cos := newRefTestCOS("ref-invalid-1", ocv1.ObjectSourceRef{
		Name:      "test-secret-invalid",
		Namespace: "olmv1-system",
		Key:       "my-key",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(secret, cos).
		WithStatusSubresource(&ocv1.ClusterObjectSet{}).
		Build()

	reconciler := &controllers.ClusterObjectSetReconciler{
		Client:                fakeClient,
		RevisionEngineFactory: newMockRevisionEngineFactoryWithEngine(mockCtrl, newNoopMockRevisionEngine(mockCtrl), nil),
		TrackingCache:         newMockTrackingCache(mockCtrl, fakeClient, nil),
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}

	// A malformed referenced manifest is invalid configuration that will not resolve on
	// retry: no error is returned, and Ready reports ValidationFailure.
	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cos.Name},
	})
	require.NoError(t, err)

	updated := &ocv1.ClusterObjectSet{}
	require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKey{Name: cos.Name}, updated))
	cond := meta.FindStatusCondition(updated.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, ocv1.ClusterObjectSetReasonValidationFailure, cond.Reason)
	assert.Contains(t, cond.Message, "unmarshal")
}

func newRefTestCOS(name string, ref ocv1.ObjectSourceRef) *ocv1.ClusterObjectSet {
	cos := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID(name),
			Labels: map[string]string{
				labels.OwnerNameKey: "test-ext",
			},
		},
		Spec: ocv1.ClusterObjectSetSpec{
			LifecycleState:      ocv1.ClusterObjectSetLifecycleStateActive,
			Revision:            1,
			CollisionProtection: ocv1.CollisionProtectionPrevent,
			Phases: []ocv1.ClusterObjectSetPhase{
				{
					Name: "deploy",
					Objects: []ocv1.ClusterObjectSetObject{
						{
							Ref: ref,
						},
					},
				},
			},
		},
	}
	cos.SetGroupVersionKind(ocv1.GroupVersion.WithKind("ClusterObjectSet"))
	return cos
}
