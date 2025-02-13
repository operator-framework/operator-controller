package core

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
	secretData := []byte(`{"auths":{"exampleRegistry": "exampledata"}}`)
	authFileName := "test-auth.json"
	for _, tt := range []struct {
		name                  string
		secret                *corev1.Secret
		addSecret             bool
		wantErr               string
		fileShouldExistBefore bool
		fileShouldExistAfter  bool
	}{
		{
			name: "secret exists, content gets saved to authFile",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-secret-namespace",
				},
				Data: map[string][]byte{
					".dockerconfigjson": secretData,
				},
			},
			addSecret:             true,
			fileShouldExistBefore: false,
			fileShouldExistAfter:  true,
		},
		{
			name: "secret does not exist, file exists previously, file should get deleted",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-secret-namespace",
				},
				Data: map[string][]byte{
					".dockerconfigjson": secretData,
				},
			},
			addSecret:             false,
			fileShouldExistBefore: true,
			fileShouldExistAfter:  false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tempAuthFile := filepath.Join(t.TempDir(), authFileName)
			clientBuilder := fake.NewClientBuilder()
			if tt.addSecret {
				clientBuilder = clientBuilder.WithObjects(tt.secret)
			}
			cl := clientBuilder.Build()

			secretKey := types.NamespacedName{Namespace: tt.secret.Namespace, Name: tt.secret.Name}
			r := &PullSecretReconciler{
				Client:       cl,
				SecretKey:    secretKey,
				AuthFilePath: tempAuthFile,
			}
			if tt.fileShouldExistBefore {
				err := os.WriteFile(tempAuthFile, secretData, 0600)
				require.NoError(t, err)
			}
			res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: secretKey})
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
