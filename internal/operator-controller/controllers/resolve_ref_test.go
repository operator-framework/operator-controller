package controllers_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

func newSchemeWithCoreV1(t *testing.T) *apimachineryruntime.Scheme {
	t.Helper()
	sch := apimachineryruntime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(sch))
	require.NoError(t, corev1.AddToScheme(sch))
	return sch
}

func TestResolveObjectRef_PlainJSON(t *testing.T) {
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
		Data: map[string][]byte{
			"my-key": cmData,
		},
	}

	cer := newRefTestCER("ref-plain-1", ocv1.ObjectSourceRef{
		Name:      "test-secret",
		Namespace: "olmv1-system",
		Key:       "my-key",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(secret, cer).
		WithStatusSubresource(&ocv1.ClusterExtensionRevision{}).
		Build()

	mockEngine := &mockRevisionEngine{
		reconcile: func(_ context.Context, _ machinerytypes.Revision, _ ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
			return mockRevisionResult{}, nil
		},
	}
	reconciler := &controllers.ClusterExtensionRevisionReconciler{
		Client:                fakeClient,
		RevisionEngineFactory: &mockRevisionEngineFactory{engine: mockEngine},
		TrackingCache:         &mockTrackingCache{client: fakeClient},
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}

	_, err = reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cer.Name},
	})
	require.NoError(t, err)
}

func TestResolveObjectRef_GzipCompressed(t *testing.T) {
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
		Data: map[string][]byte{
			"my-key": buf.Bytes(),
		},
	}

	cer := newRefTestCER("ref-gzip-1", ocv1.ObjectSourceRef{
		Name:      "test-secret-gz",
		Namespace: "olmv1-system",
		Key:       "my-key",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(secret, cer).
		WithStatusSubresource(&ocv1.ClusterExtensionRevision{}).
		Build()

	mockEngine := &mockRevisionEngine{
		reconcile: func(_ context.Context, _ machinerytypes.Revision, _ ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
			return mockRevisionResult{}, nil
		},
	}
	reconciler := &controllers.ClusterExtensionRevisionReconciler{
		Client:                fakeClient,
		RevisionEngineFactory: &mockRevisionEngineFactory{engine: mockEngine},
		TrackingCache:         &mockTrackingCache{client: fakeClient},
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}

	_, err = reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cer.Name},
	})
	require.NoError(t, err)
}

func TestResolveObjectRef_SecretNotFound(t *testing.T) {
	testScheme := newSchemeWithCoreV1(t)

	cer := newRefTestCER("ref-notfound-1", ocv1.ObjectSourceRef{
		Name:      "nonexistent-secret",
		Namespace: "olmv1-system",
		Key:       "my-key",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(cer).
		WithStatusSubresource(&ocv1.ClusterExtensionRevision{}).
		Build()

	reconciler := &controllers.ClusterExtensionRevisionReconciler{
		Client:                fakeClient,
		RevisionEngineFactory: &mockRevisionEngineFactory{engine: &mockRevisionEngine{}},
		TrackingCache:         &mockTrackingCache{client: fakeClient},
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cer.Name},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving ref")
}

func TestResolveObjectRef_KeyNotFound(t *testing.T) {
	testScheme := newSchemeWithCoreV1(t)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret-nokey",
			Namespace: "olmv1-system",
		},
		Data: map[string][]byte{
			"other-key": []byte("{}"),
		},
	}

	cer := newRefTestCER("ref-nokey-1", ocv1.ObjectSourceRef{
		Name:      "test-secret-nokey",
		Namespace: "olmv1-system",
		Key:       "missing-key",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(secret, cer).
		WithStatusSubresource(&ocv1.ClusterExtensionRevision{}).
		Build()

	reconciler := &controllers.ClusterExtensionRevisionReconciler{
		Client:                fakeClient,
		RevisionEngineFactory: &mockRevisionEngineFactory{engine: &mockRevisionEngine{}},
		TrackingCache:         &mockTrackingCache{client: fakeClient},
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cer.Name},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key")
}

func TestResolveObjectRef_InvalidJSON(t *testing.T) {
	testScheme := newSchemeWithCoreV1(t)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret-invalid",
			Namespace: "olmv1-system",
		},
		Data: map[string][]byte{
			"my-key": []byte("not-valid-json"),
		},
	}

	cer := newRefTestCER("ref-invalid-1", ocv1.ObjectSourceRef{
		Name:      "test-secret-invalid",
		Namespace: "olmv1-system",
		Key:       "my-key",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(secret, cer).
		WithStatusSubresource(&ocv1.ClusterExtensionRevision{}).
		Build()

	reconciler := &controllers.ClusterExtensionRevisionReconciler{
		Client:                fakeClient,
		RevisionEngineFactory: &mockRevisionEngineFactory{engine: &mockRevisionEngine{}},
		TrackingCache:         &mockTrackingCache{client: fakeClient},
		Clock:                 clocktesting.NewFakeClock(metav1.Now().Time),
	}

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cer.Name},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func newRefTestCER(name string, ref ocv1.ObjectSourceRef) *ocv1.ClusterExtensionRevision {
	cer := &ocv1.ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID(name),
			Labels: map[string]string{
				labels.OwnerNameKey: "test-ext",
			},
		},
		Spec: ocv1.ClusterExtensionRevisionSpec{
			LifecycleState:      ocv1.ClusterExtensionRevisionLifecycleStateActive,
			Revision:            1,
			CollisionProtection: ocv1.CollisionProtectionPrevent,
			Phases: []ocv1.ClusterExtensionRevisionPhase{
				{
					Name: "deploy",
					Objects: []ocv1.ClusterExtensionRevisionObject{
						{
							Ref: ref,
						},
					},
				},
			},
		},
	}
	cer.SetGroupVersionKind(ocv1.GroupVersion.WithKind("ClusterExtensionRevision"))
	return cer
}
