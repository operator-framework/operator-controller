package controllers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSecretSyncerReconciler(t *testing.T) {
	secretFullData := []byte(`{"auths":{"exampleRegistry": {"auth": "exampledata"}}}`)
	secretPartData := []byte(`{"exampleRegistry": {"auth": "exampledata"}}`)
	authFileName := "test-auth.json"
	for _, tt := range []struct {
		name                  string
		secretKey             *types.NamespacedName
		sa                    *corev1.ServiceAccount
		secrets               []corev1.Secret
		wantErr               string
		fileShouldExistBefore bool
		fileShouldExistAfter  bool
	}{
		{
			name:      "secret exists, dockerconfigjson content gets saved to authFile",
			secretKey: &types.NamespacedName{Namespace: "test-secret-namespace", Name: "test-secret"},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "test-secret-namespace",
					},
					Data: map[string][]byte{
						".dockerconfigjson": secretFullData,
					},
				},
			},
			fileShouldExistBefore: false,
			fileShouldExistAfter:  true,
		},
		{
			name:      "secret exists, dockercfg content gets saved to authFile",
			secretKey: &types.NamespacedName{Namespace: "test-secret-namespace", Name: "test-secret"},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "test-secret-namespace",
					},
					Data: map[string][]byte{
						".dockercfg": secretPartData,
					},
				},
			},
			fileShouldExistBefore: false,
			fileShouldExistAfter:  true,
		},
		{
			name:                  "secret does not exist, file exists previously, file should get deleted",
			secretKey:             &types.NamespacedName{Namespace: "test-secret-namespace", Name: "test-secret"},
			fileShouldExistBefore: true,
			fileShouldExistAfter:  false,
		},
		{
			name: "serviceaccount secrets, both dockerconfigjson and dockercfg content gets saved to authFile",
			sa: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sa",
					Namespace: "test-secret-namespace",
				},
				ImagePullSecrets: []corev1.LocalObjectReference{
					{Name: "test-secret1"},
					{Name: "test-secret2"},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret1",
						Namespace: "test-secret-namespace",
					},
					Data: map[string][]byte{
						".dockerconfigjson": secretFullData,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret2",
						Namespace: "test-secret-namespace",
					},
					Data: map[string][]byte{
						".dockerconfigjson": secretFullData,
					},
				},
			},
			fileShouldExistBefore: false,
			fileShouldExistAfter:  true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tempAuthFile := filepath.Join(t.TempDir(), authFileName)
			clientBuilder := fake.NewClientBuilder()
			for _, ps := range tt.secrets {
				clientBuilder = clientBuilder.WithObjects(ps.DeepCopy())
			}
			if tt.sa != nil {
				clientBuilder = clientBuilder.WithObjects(tt.sa)
			}
			cl := clientBuilder.Build()

			var triggerKey types.NamespacedName
			if tt.secretKey != nil {
				triggerKey = *tt.secretKey
			}
			var saKey types.NamespacedName
			if tt.sa != nil {
				saKey = types.NamespacedName{Namespace: tt.sa.Namespace, Name: tt.sa.Name}
				triggerKey = saKey
			}
			r := &PullSecretReconciler{
				Client:            cl,
				SecretKey:         tt.secretKey,
				ServiceAccountKey: saKey,
				AuthFilePath:      tempAuthFile,
			}
			if tt.fileShouldExistBefore {
				err := os.WriteFile(tempAuthFile, secretFullData, 0600)
				require.NoError(t, err)
			}
			res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: triggerKey})
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
			require.Equal(t, ctrl.Result{}, res)

			if tt.fileShouldExistAfter {
				_, err := os.Stat(tempAuthFile)
				require.NoError(t, err)
			} else {
				_, err := os.Stat(tempAuthFile)
				require.True(t, os.IsNotExist(err))
			}
		})
	}
}
