package v1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// DeepCopyInto copies the receiver into out. in must be non-nil.
func (in *ClusterExtensionConfig) DeepCopyInto(out *ClusterExtensionConfig) {
	*out = *in
	if in.Inline != nil {
		out.Inline = make(map[string]apiextensionsv1.JSON, len(in.Inline))
		for k, v := range in.Inline {
			if v.Raw != nil {
				out.Inline[k] = apiextensionsv1.JSON{Raw: append([]byte(nil), v.Raw...)}
			} else {
				out.Inline[k] = apiextensionsv1.JSON{}
			}
		}
	}
	if in.SecretRef != nil {
		out.SecretRef = new(corev1.SecretKeySelector)
		in.SecretRef.DeepCopyInto(out.SecretRef)
	}
}

// DeepCopy creates a new ClusterExtensionConfig by copying the receiver.
func (in *ClusterExtensionConfig) DeepCopy() *ClusterExtensionConfig {
	if in == nil {
		return nil
	}
	out := new(ClusterExtensionConfig)
	in.DeepCopyInto(out)
	return out
}
