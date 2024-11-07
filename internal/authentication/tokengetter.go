package authentication

import (
	"context"
	"sync"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/utils/ptr"
)

type TokenGetter struct {
	client             corev1.ServiceAccountsGetter
	expirationDuration time.Duration
	tokens             map[types.NamespacedName]*authenticationv1.TokenRequestStatus
	mu                 sync.RWMutex
}

type TokenGetterOption func(*TokenGetter)

const (
	rotationThresholdFraction = 0.1
	DefaultExpirationDuration = 5 * time.Minute
)

// Returns a token getter that can fetch tokens given a service account.
// The token getter also caches tokens which helps reduce the number of requests to the API Server.
// In case a cached token is expiring a fresh token is created.
func NewTokenGetter(client corev1.ServiceAccountsGetter, options ...TokenGetterOption) *TokenGetter {
	tokenGetter := &TokenGetter{
		client:             client,
		expirationDuration: DefaultExpirationDuration,
		tokens:             map[types.NamespacedName]*authenticationv1.TokenRequestStatus{},
	}

	for _, opt := range options {
		opt(tokenGetter)
	}

	return tokenGetter
}

func WithExpirationDuration(expirationDuration time.Duration) TokenGetterOption {
	return func(tg *TokenGetter) {
		tg.expirationDuration = expirationDuration
	}
}

// Get returns a token from the cache if available and not expiring, otherwise creates a new token
func (t *TokenGetter) Get(ctx context.Context, key types.NamespacedName) (string, error) {
	t.mu.RLock()
	token, ok := t.tokens[key]
	t.mu.RUnlock()

	expireTime := time.Time{}
	if ok {
		expireTime = token.ExpirationTimestamp.Time
	}

	// Create a new token if the cached token expires within rotationThresholdFraction of expirationDuration from now
	rotationThresholdAfterNow := metav1.Now().Add(time.Duration(float64(t.expirationDuration) * (rotationThresholdFraction)))
	if expireTime.Before(rotationThresholdAfterNow) {
		var err error
		token, err = t.getToken(ctx, key)
		if err != nil {
			return "", err
		}
		t.mu.Lock()
		t.tokens[key] = token
		t.mu.Unlock()
	}

	// Delete tokens that have expired
	t.reapExpiredTokens()

	return token.Token, nil
}

func (t *TokenGetter) getToken(ctx context.Context, key types.NamespacedName) (*authenticationv1.TokenRequestStatus, error) {
	req, err := t.client.ServiceAccounts(key.Namespace).CreateToken(ctx,
		key.Name,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: ptr.To(int64(t.expirationDuration / time.Second))},
		}, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return &req.Status, nil
}

func (t *TokenGetter) reapExpiredTokens() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, token := range t.tokens {
		if metav1.Now().Sub(token.ExpirationTimestamp.Time) > 0 {
			delete(t.tokens, key)
		}
	}
}

func (t *TokenGetter) Delete(key types.NamespacedName) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.tokens, key)
}
