package hash

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
)

// DeepHashObject writes specified object to hash based on the object's
// JSON representation. If the object fails JSON marshaling, DeepHashObject
// panics.
func DeepHashObject(obj interface{}) string {
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
	encoder := json.NewEncoder(hasher)
	if err := encoder.Encode(obj); err != nil {
		panic(fmt.Sprintf("couldn't encode object: %v", err))
	}

	// TODO: Investigate whether we can change this to base62(sha224(bytes))
	//   The main concern with changing the base is that it will cause the hash
	//   function output to change, which may cause issues with consumers expecting
	//   a consistent hash.
	//
	// base36(sha224(bytes)) is a useful hash and encoding for adding the contents of this
	// to a Kubernetes identifier or other field which has length and character set requirements
	var i big.Int
	i.SetBytes(hasher.Sum(nil))
	return i.Text(36)
}
