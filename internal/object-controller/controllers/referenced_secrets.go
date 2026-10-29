//go:build !standard

package controllers

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
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

// mutableSecretError distinguishes a confirmed mutable Secret from a transient
// read error, so reconciliation can report an actionable precondition failure.
type mutableSecretError struct {
	msg string
}

func (e *mutableSecretError) Error() string { return e.msg }

// verifyImmutable checks every distinct referenced Secret. Confirmed mutable
// Secrets take precedence over read errors; decoding must use this same reader.
func (r *referencedSecretReader) verifyImmutable(ctx context.Context, cos *ocv1.ClusterObjectSet) error {
	seen := sets.New[client.ObjectKey]()
	var mutableSecrets []string
	var readErr error

	for _, phase := range cos.Spec.Phases {
		for _, obj := range phase.Objects {
			if obj.Ref.Name == "" {
				continue
			}
			key := client.ObjectKey{Name: obj.Ref.Name, Namespace: obj.Ref.Namespace}
			if seen.Has(key) {
				continue
			}
			seen.Insert(key)
			secret, err := r.get(ctx, key)
			if err != nil {
				// Keep checking so a confirmed mutable Secret takes precedence over a read error.
				if readErr == nil {
					readErr = fmt.Errorf("getting Secret %s/%s: %w", key.Namespace, key.Name, err)
				}
				continue
			}

			if secret.Immutable == nil || !*secret.Immutable {
				mutableSecrets = append(mutableSecrets, fmt.Sprintf("%s/%s", key.Namespace, key.Name))
			}
		}
	}

	if len(mutableSecrets) > 0 {
		return &mutableSecretError{msg: fmt.Sprintf(
			"Referenced Secret(s) must be immutable but are not: %s. Recreate them with immutable set to true.",
			strings.Join(mutableSecrets, ", "))}
	}

	return readErr
}

// referencedContentError marks invalid Secret content, such as a missing key or
// malformed manifest. Replacing the source can clear it without changing the ref.
type referencedContentError struct{ error }

// resolveObjectRef decodes the verified Secret snapshot, auto-detecting gzip
// compression, into an unstructured Kubernetes object.
func (r *referencedSecretReader) resolveObjectRef(ctx context.Context, ref ocv1.ObjectSourceRef) (*unstructured.Unstructured, error) {
	key := client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}
	secret, err := r.get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("getting Secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}

	data, ok := secret.Data[ref.Key]
	if !ok {
		return nil, &referencedContentError{fmt.Errorf("key %q not found in Secret %s/%s", ref.Key, ref.Namespace, ref.Name)}
	}

	// Auto-detect gzip compression (magic bytes 0x1f 0x8b).
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, &referencedContentError{fmt.Errorf("creating gzip reader for key %q in Secret %s/%s: %w", ref.Key, ref.Namespace, ref.Name, err)}
		}
		defer reader.Close()
		const maxDecompressedSize = 10 * 1024 * 1024 // 10 MiB
		limited := io.LimitReader(reader, maxDecompressedSize+1)
		decompressed, err := io.ReadAll(limited)
		if err != nil {
			return nil, &referencedContentError{fmt.Errorf("decompressing key %q in Secret %s/%s: %w", ref.Key, ref.Namespace, ref.Name, err)}
		}
		if len(decompressed) > maxDecompressedSize {
			return nil, &referencedContentError{fmt.Errorf("decompressed data for key %q in Secret %s/%s exceeds maximum size (%d bytes)", ref.Key, ref.Namespace, ref.Name, maxDecompressedSize)}
		}
		data = decompressed
	}

	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(data, &obj.Object); err != nil {
		return nil, &referencedContentError{fmt.Errorf("unmarshaling object from key %q in Secret %s/%s: %w", ref.Key, ref.Namespace, ref.Name, err)}
	}

	return obj, nil
}
