package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestSecretClientReadsCurrentSecrets(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(testScheme))
	for _, namespace := range []string{"olmv1-system", "extension"} {
		for _, deleted := range []bool{false, true} {
			name := namespace + "/updated"
			if deleted {
				name = namespace + "/deleted"
			}
			t.Run(name, func(t *testing.T) {
				stale := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "content", Namespace: namespace},
					Data:       map[string][]byte{"object": []byte("old content")},
				}
				cachedClient := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(stale).Build()
				apiBuilder := fake.NewClientBuilder().WithScheme(testScheme)
				if !deleted {
					current := stale.DeepCopy()
					current.Data["object"] = []byte("new content")
					apiBuilder.WithObjects(current)
				}
				cl := &uncachedSecretClient{
					Client: cachedClient, apiReader: apiBuilder.Build(),
				}
				secret := &corev1.Secret{}
				err := cl.Get(t.Context(), client.ObjectKeyFromObject(stale), secret)
				if deleted {
					require.True(t, apierrors.IsNotFound(err), "must not return a cached Secret after deletion, got %v", err)
					return
				}
				require.NoError(t, err)
				require.Equal(t, []byte("new content"), secret.Data["object"])
			})
		}
	}
}

func TestSecretClientUsesCacheForOtherObjects(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(testScheme))
	cached := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{Name: "revision"},
		Spec:       ocv1.ClusterObjectSetSpec{Revision: 1},
	}
	current := cached.DeepCopy()
	current.Spec.Revision = 2
	cl := &uncachedSecretClient{
		Client:    fake.NewClientBuilder().WithScheme(testScheme).WithObjects(cached).Build(),
		apiReader: fake.NewClientBuilder().WithScheme(testScheme).WithObjects(current).Build(),
	}
	cos := &ocv1.ClusterObjectSet{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(cached), cos))
	require.Equal(t, int64(1), cos.Spec.Revision)
}
