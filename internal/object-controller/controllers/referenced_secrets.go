package controllers

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// referencedSecretReader shares a Secret snapshot between verification and object
// decoding within one reconciliation. A new reader must be used on the next
// reconciliation so that deleted and recreated Secrets are read again.
type referencedSecretReader struct {
	reader  client.Reader
	secrets map[client.ObjectKey]*corev1.Secret
}

func newReferencedSecretReader(reader client.Reader) *referencedSecretReader {
	return &referencedSecretReader{reader: reader, secrets: make(map[client.ObjectKey]*corev1.Secret)}
}

func (r *referencedSecretReader) get(ctx context.Context, key client.ObjectKey) (*corev1.Secret, error) {
	if secret, ok := r.secrets[key]; ok {
		return secret, nil
	}
	secret := &corev1.Secret{}
	if err := r.reader.Get(ctx, key, secret); err != nil {
		return nil, err
	}
	r.secrets[key] = secret
	return secret, nil
}

type mutableSecretsError struct {
	names []string
}

func (e *mutableSecretsError) Error() string {
	return fmt.Sprintf("the following secrets are not immutable (referenced secrets must have immutable set to true): %s", strings.Join(e.names, ", "))
}
