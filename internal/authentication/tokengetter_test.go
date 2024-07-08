package authentication

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ctest "k8s.io/client-go/testing"
)

func TestTokenGetterGet(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	fakeClient.PrependReactor("create", "serviceaccounts/token",
		func(action ctest.Action) (bool, runtime.Object, error) {
			act, ok := action.(ctest.CreateActionImpl)
			if !ok {
				return false, nil, nil
			}
			tokenRequest := act.GetObject().(*authenticationv1.TokenRequest)
			var err error
			if act.Name == "test-service-account-1" {
				tokenRequest.Status = authenticationv1.TokenRequestStatus{
					Token:               "test-token-1",
					ExpirationTimestamp: metav1.NewTime(metav1.Now().Add(DefaultExpirationDuration)),
				}
			}
			if act.Name == "test-service-account-2" {
				tokenRequest.Status = authenticationv1.TokenRequestStatus{
					Token:               "test-token-2",
					ExpirationTimestamp: metav1.NewTime(metav1.Now().Add(1 * time.Second)),
				}
			}
			if act.Name == "test-service-account-3" {
				tokenRequest.Status = authenticationv1.TokenRequestStatus{
					Token:               "test-token-3",
					ExpirationTimestamp: metav1.NewTime(metav1.Now().Add(-10 * time.Second)),
				}
			}
			if act.Name == "test-service-account-4" {
				tokenRequest = nil
				err = fmt.Errorf("error when fetching token")
			}
			return true, tokenRequest, err
		})

	tg := NewTokenGetter(fakeClient.CoreV1(),
		WithExpirationDuration(DefaultExpirationDuration))

	tests := []struct {
		testName           string
		serviceAccountName string
		namespace          string
		want               string
		errorMsg           string
	}{
		{"Testing getting token with fake client", "test-service-account-1",
			"test-namespace-1", "test-token-1", "failed to get token"},
		{"Testing getting token from cache", "test-service-account-1",
			"test-namespace-1", "test-token-1", "failed to get token"},
		{"Testing getting short lived token from fake client", "test-service-account-2",
			"test-namespace-2", "test-token-2", "failed to get token"},
		{"Testing getting nearly expired token from cache", "test-service-account-2",
			"test-namespace-2", "test-token-2", "failed to refresh token"},
		{"Testing token that expired 10 seconds ago", "test-service-account-3",
			"test-namespace-3", "test-token-3", "failed to get token"},
		{"Testing error when getting token from fake client", "test-service-account-4",
			"test-namespace-4", "error when fetching token", "error when fetching token"},
	}

	for _, tc := range tests {
		got, err := tg.Get(context.Background(), types.NamespacedName{Namespace: tc.namespace, Name: tc.serviceAccountName})
		if err != nil {
			t.Logf("%s: expected: %v, got: %v", tc.testName, tc.want, err)
			assert.EqualError(t, err, tc.errorMsg)
		} else {
			t.Logf("%s: expected: %v, got: %v", tc.testName, tc.want, got)
			assert.Equal(t, tc.want, got, tc.errorMsg)
		}
	}
}
