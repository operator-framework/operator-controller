package authentication

import (
	"context"
	"sync"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/utils/ptr"
)

type TokenGetter struct {
	client            corev1.ServiceAccountsGetter
	expirationSeconds int64
	tokens            map[types.NamespacedName]*authv1.TokenRequestStatus
	tokenLocks        keyLock[types.NamespacedName]
	mu                sync.RWMutex
}

// Returns a token getter that can fetch tokens given a service account.
// The token getter also caches tokens which helps reduce the number of requests to the API Server.
// In case a cached token is expiring a fresh token is created.
func NewTokenGetter(client corev1.ServiceAccountsGetter, expirationSeconds int64) *TokenGetter {
	return &TokenGetter{
		client:            client,
		expirationSeconds: expirationSeconds,
		tokens:            map[types.NamespacedName]*authv1.TokenRequestStatus{},
		tokenLocks:        newKeyLock[types.NamespacedName](),
	}
}

type keyLock[K comparable] struct {
	locks map[K]*sync.Mutex
	mu    sync.Mutex
}

func newKeyLock[K comparable]() keyLock[K] {
	return keyLock[K]{locks: map[K]*sync.Mutex{}}
}

func (k *keyLock[K]) Lock(key K) {
	k.getLock(key).Lock()
}

func (k *keyLock[K]) Unlock(key K) {
	k.getLock(key).Unlock()
}

func (k *keyLock[K]) getLock(key K) *sync.Mutex {
	k.mu.Lock()
	defer k.mu.Unlock()

	lock, ok := k.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		k.locks[key] = lock
	}
	return lock
}

// Returns a token from the cache if available and not expiring, otherwise creates a new token and caches it.
func (t *TokenGetter) Get(ctx context.Context, key types.NamespacedName) (string, error) {
	t.tokenLocks.Lock(key)
	defer t.tokenLocks.Unlock(key)

	t.mu.RLock()
	token, ok := t.tokens[key]
	t.mu.RUnlock()

	expireTime := time.Time{}
	if ok {
		expireTime = token.ExpirationTimestamp.Time
	}

	fiveMinutesAfterNow := metav1.Now().Add(5 * time.Minute)
	if expireTime.Before(fiveMinutesAfterNow) {
		var err error
		token, err = t.getToken(ctx, key)
		if err != nil {
			return "", err
		}
		t.mu.Lock()
		t.tokens[key] = token
		t.mu.Unlock()
	}

	return token.Token, nil
}

func (t *TokenGetter) getToken(ctx context.Context, key types.NamespacedName) (*authv1.TokenRequestStatus, error) {
	req, err := t.client.ServiceAccounts(key.Namespace).CreateToken(ctx, key.Name, &authv1.TokenRequest{Spec: authv1.TokenRequestSpec{ExpirationSeconds: ptr.To[int64](3600)}}, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return &req.Status, nil
}
