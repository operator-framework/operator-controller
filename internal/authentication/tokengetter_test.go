package authentication

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ctest "k8s.io/client-go/testing"
)

func TestNewTokenGetter(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	fakeClient.PrependReactor("create", "serviceaccounts/token", func(action ctest.Action) (handled bool, ret runtime.Object, err error) {
		act, ok := action.(ctest.CreateActionImpl)
		if !ok {
			return false, nil, nil
		}
		tokenRequest := act.GetObject().(*authv1.TokenRequest)
		if act.Name == "test-service-account-1" {
			tokenRequest.Status = authv1.TokenRequestStatus{
				Token:               "test-token-1",
				ExpirationTimestamp: metav1.NewTime(metav1.Now().Add(5 * time.Minute)),
			}
		}
		if act.Name == "test-service-account-2" {
			tokenRequest.Status = authv1.TokenRequestStatus{
				Token:               "test-token-2",
				ExpirationTimestamp: metav1.NewTime(metav1.Now().Add(1 * time.Second)),
			}
		}
		if act.Name == "test-service-account-3" {
			tokenRequest = nil
			err = fmt.Errorf("error when fetching token")
		}

		return true, tokenRequest, err
	})
	tg := NewTokenGetter(fakeClient.CoreV1(), int64(5*time.Minute))
	t.Log("Testing NewTokenGetter with fake client")
	token, err := tg.Get(context.Background(), types.NamespacedName{
		Namespace: "test-namespace-1",
		Name:      "test-service-account-1",
	})
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
		return
	}
	t.Log("token:", token)
	if token != "test-token-1" {
		t.Errorf("token does not match")
	}
	t.Log("Testing getting token from cache")
	token, err = tg.Get(context.Background(), types.NamespacedName{
		Namespace: "test-namespace-1",
		Name:      "test-service-account-1",
	})
	if err != nil {
		t.Fatalf("failed to get token from cache: %v", err)
		return
	}
	t.Log("token:", token)
	if token != "test-token-1" {
		t.Errorf("token does not match")
	}
	t.Log("Testing getting short lived token from fake client")
	token, err = tg.Get(context.Background(), types.NamespacedName{
		Namespace: "test-namespace-2",
		Name:      "test-service-account-2",
	})
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
		return
	}
	t.Log("token:", token)
	if token != "test-token-2" {
		t.Errorf("token does not match")
	}
	//wait for token to expire
	time.Sleep(1 * time.Second)
	t.Log("Testing getting expired token from cache")
	token, err = tg.Get(context.Background(), types.NamespacedName{
		Namespace: "test-namespace-2",
		Name:      "test-service-account-2",
	})
	if err != nil {
		t.Fatalf("failed to refresh token: %v", err)
		return
	}
	t.Log("token:", token)
	if token != "test-token-2" {
		t.Errorf("token does not match")
	}
	t.Log("Testing error when getting token from fake client")
	token, err = tg.Get(context.Background(), types.NamespacedName{
		Namespace: "test-namespace-3",
		Name:      "test-service-account-3",
	})
	assert.EqualError(t, err, "error when fetching token")
}
