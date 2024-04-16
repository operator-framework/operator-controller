package util

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
)

// DeepHashObject writes specified object to hash using the spew library
// which follows pointers and prints actual values of the nested objects
// ensuring the hash does not change when a pointer changes.
func DeepHashObject(obj interface{}) (string, error) {
	// While the most accurate encoding we could do for Kubernetes objects (runtime.Object)
	// would use the API machinery serializers, those operate over entire objects - and
	// we often need to operate on snippets. Checking with the experts and the implementation,
	// we can see that the serializers are a thin wrapper over json.Marshal for encoding:
	// https://github.com/kubernetes/kubernetes/blob/8509ab82b96caa2365552efa08c8ba8baf11c5ec/staging/src/k8s.io/apimachinery/pkg/runtime/serializer/json/json.go#L216-L247
	// Therefore, we can be confident that using json.Marshal() here will:
	//  1. be stable & idempotent - the library sorts keys, etc.
	//  2. be germane to our needs - only fields that serialize and are sent to the server
	//     will be encoded

	hasher := sha256.New224()
	hasher.Reset()
	encoder := json.NewEncoder(hasher)
	if err := encoder.Encode(obj); err != nil {
		return "", fmt.Errorf("couldn't encode object: %w", err)
	}

	// base62(sha224(bytes)) is a useful hash and encoding for adding the contents of this
	// to a Kubernetes identifier or other field which has length and character set requirements
	var hash []byte
	hash = hasher.Sum(hash)

	var i big.Int
	i.SetBytes(hash[:])
	return i.Text(36), nil
}
