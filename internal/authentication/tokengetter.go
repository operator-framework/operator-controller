package authentication

import (
	"context"
	"sync"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/utils/ptr"
)

type TokenGetter struct {
	client            corev1client.ServiceAccountsGetter
	expirationSeconds int64
	tokens            map[types.NamespacedName]*authenticationv1.TokenRequestStatus
	tokenLocks        keyLock[types.NamespacedName]
	mu                sync.RWMutex
}

func NewTokenGetter(client corev1client.ServiceAccountsGetter, expirationSeconds int64) *TokenGetter {
	return &TokenGetter{
		client:            client,
		expirationSeconds: expirationSeconds,
		tokenLocks:        newKeyLock[types.NamespacedName](),
		tokens:            map[types.NamespacedName]*authenticationv1.TokenRequestStatus{},
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

func (t *TokenGetter) getToken(ctx context.Context, key types.NamespacedName) (*authenticationv1.TokenRequestStatus, error) {
	req, err := t.client.ServiceAccounts(key.Namespace).CreateToken(ctx, key.Name, &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{
		ExpirationSeconds: ptr.To[int64](3600),
	}}, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return &req.Status, nil
}
