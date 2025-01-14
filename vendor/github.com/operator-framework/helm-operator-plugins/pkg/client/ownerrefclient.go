package client

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var _ clientcorev1.SecretInterface = &ownerRefSecretClient{}

// NewOwnerRefSecretClient returns a SecretInterface that injects the provided owner references
// to all created or updated secrets that match the provided match function. If match is nil, all
// secrets are matched.
func NewOwnerRefSecretClient(client clientcorev1.SecretInterface, refs []metav1.OwnerReference, match func(*corev1.Secret) bool) clientcorev1.SecretInterface {
	if match == nil {
		match = MatchAllSecrets
	}
	return &ownerRefSecretClient{
		SecretInterface: client,
		match:           match,
		refs:            refs,
	}
}

func MatchAllSecrets(_ *corev1.Secret) bool {
	return true
}

type ownerRefSecretClient struct {
	clientcorev1.SecretInterface
	match func(secret *corev1.Secret) bool
	refs  []metav1.OwnerReference
}

func (c *ownerRefSecretClient) appendMissingOwnerRefs(secret *corev1.Secret) {
	hasOwnerRef := func(secret *corev1.Secret, ref metav1.OwnerReference) bool {
		for _, r := range secret.OwnerReferences {
			if r.UID == ref.UID {
				return true
			}
		}
		return false
	}
	for i := range c.refs {
		if !hasOwnerRef(secret, c.refs[i]) {
			secret.OwnerReferences = append(secret.OwnerReferences, c.refs[i])
		}
	}
}

func (c *ownerRefSecretClient) Create(ctx context.Context, in *corev1.Secret, opts metav1.CreateOptions) (*corev1.Secret, error) {
	if c.match == nil || c.match(in) {
		c.appendMissingOwnerRefs(in)
	}
	return c.SecretInterface.Create(ctx, in, opts)
}

func (c *ownerRefSecretClient) Update(ctx context.Context, in *corev1.Secret, opts metav1.UpdateOptions) (*corev1.Secret, error) {
	if c.match == nil || c.match(in) {
		c.appendMissingOwnerRefs(in)
	}
	return c.SecretInterface.Update(ctx, in, opts)
}
