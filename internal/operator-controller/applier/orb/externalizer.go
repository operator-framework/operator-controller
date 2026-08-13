package orb

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	orbac "github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
)

const (
	// maxDataSize is the target maximum for serialized resource data.
	// 900 KiB leaves headroom for apiserver-added fields (uid,
	// creationTimestamp, generation, managedFields, status) within
	// etcd's ~1.5 MiB limit.
	maxDataSize = 900 * 1024

	// maxObjectsPerSlice is the API maximum for SliceObject entries
	// in a single ClusterObjectSlice.
	maxObjectsPerSlice = 256
)

// ExternalizeCOD checks whether the COD apply configuration would exceed the
// safe etcd size threshold. If it would, all inline objects are packed into
// ClusterObjectSlice apply configurations and the COD is rewritten to use
// objectRef entries. If the COD is small enough, it is returned unchanged
// with a nil slice list.
//
// Each produced COSL inherits the COD's metadata labels (e.g. the owner
// labels) and owner references, so callers can discover a COD's slices by
// label selector and so the slices are garbage-collected / watched alongside
// the COD's owner (the ClusterExtension).
func ExternalizeCOD(
	cod *orbac.ClusterObjectDeploymentApplyConfiguration,
) (*orbac.ClusterObjectDeploymentApplyConfiguration, []*orbac.ClusterObjectSliceApplyConfiguration, error) {
	needed, err := shouldExternalize(cod)
	if err != nil {
		return nil, nil, err
	}
	if !needed {
		return cod, nil, nil
	}

	codName := ""
	if n := cod.GetName(); n != nil {
		codName = *n
	}

	packer := &slicePacker{codName: codName}
	result, err := packer.pack(cod.Spec.Template.Spec.Phases)
	if err != nil {
		return nil, nil, err
	}

	// Propagate the COD's labels (owner labels, etc.) and owner references onto
	// each slice so the slices are discoverable by the same selector used to
	// find the COD, and are garbage-collected / watched alongside the COD's
	// owner (the ClusterExtension).
	ownerRefs := codOwnerReferences(cod)
	for _, slice := range result.slices {
		slice.WithLabels(codLabels(cod))
		if len(ownerRefs) > 0 {
			slice.WithOwnerReferences(ownerRefs...)
		}
	}

	replaceInlineWithRefs(cod, result)
	return cod, result.slices, nil
}

// codLabels returns the COD's metadata labels, or nil if none are set.
func codLabels(cod *orbac.ClusterObjectDeploymentApplyConfiguration) map[string]string {
	if cod.ObjectMetaApplyConfiguration == nil {
		return nil
	}
	return cod.Labels
}

// codOwnerReferences returns pointers to the COD's owner references so they can
// be copied onto each ClusterObjectSlice.
func codOwnerReferences(cod *orbac.ClusterObjectDeploymentApplyConfiguration) []*metav1ac.OwnerReferenceApplyConfiguration {
	if cod.ObjectMetaApplyConfiguration == nil {
		return nil
	}
	refs := make([]*metav1ac.OwnerReferenceApplyConfiguration, 0, len(cod.OwnerReferences))
	for i := range cod.OwnerReferences {
		ref := cod.OwnerReferences[i]
		refs = append(refs, &ref)
	}
	return refs
}

func shouldExternalize(cod *orbac.ClusterObjectDeploymentApplyConfiguration) (bool, error) {
	data, err := json.Marshal(cod)
	if err != nil {
		return false, fmt.Errorf("estimating COD size: %w", err)
	}
	return len(data) > maxDataSize, nil
}

type slicePacker struct {
	codName string
}

type slicePackResult struct {
	slices []*orbac.ClusterObjectSliceApplyConfiguration
	refs   map[[2]int]*orbac.ObjectRefApplyConfiguration
}

func (p *slicePacker) pack(phases []orbac.PhaseApplyConfiguration) (*slicePackResult, error) {
	result := &slicePackResult{
		refs: make(map[[2]int]*orbac.ObjectRefApplyConfiguration),
	}

	type pendingEntry struct {
		pos      [2]int
		identity objectIdentity
	}

	var (
		currentObjects []orbac.SliceObjectApplyConfiguration
		currentCount   int32
		currentSize    int
		currentPending []pendingEntry
	)

	finalizeCurrent := func() {
		if currentCount == 0 {
			return
		}
		sliceName := p.sliceNameFromObjects(currentObjects)
		slice := orbac.ClusterObjectSlice(sliceName).
			WithCount(currentCount)
		ptrs := make([]*orbac.SliceObjectApplyConfiguration, len(currentObjects))
		for i := range currentObjects {
			ptrs[i] = &currentObjects[i]
		}
		slice.WithObjects(ptrs...)
		result.slices = append(result.slices, slice)

		for _, pe := range currentPending {
			result.refs[pe.pos] = orbac.ObjectRef().
				WithSliceName(sliceName).
				WithAPIVersion(pe.identity.apiVersion).
				WithKind(pe.identity.kind).
				WithName(pe.identity.name).
				WithNamespace(pe.identity.namespace)
		}
		currentObjects = nil
		currentCount = 0
		currentSize = 0
		currentPending = nil
	}

	for phaseIdx := range phases {
		phase := &phases[phaseIdx]
		for objIdx, obj := range phase.Objects {
			if obj.Object == nil || len(obj.Object.Raw) == 0 {
				continue
			}

			id, err := parseObjectIdentity(obj.Object.Raw)
			if err != nil {
				phaseName := "<unnamed>"
				if phase.Name != nil {
					phaseName = *phase.Name
				}
				return nil, fmt.Errorf("phase %q object %d: %w", phaseName, objIdx, err)
			}

			content, err := gzipData(obj.Object.Raw)
			if err != nil {
				return nil, fmt.Errorf("compressing phase %d object %d: %w", phaseIdx, objIdx, err)
			}

			if len(content) > maxDataSize {
				return nil, fmt.Errorf(
					"object in phase %d index %d exceeds maximum data size (%d bytes > %d bytes) even after compression",
					phaseIdx, objIdx, len(content), maxDataSize,
				)
			}

			if (currentSize+len(content) > maxDataSize || currentCount >= maxObjectsPerSlice) && currentCount > 0 {
				finalizeCurrent()
			}

			so := orbac.SliceObject()
			so.WithAPIVersion(id.apiVersion)
			so.WithKind(id.kind)
			so.WithName(id.name)
			so.WithNamespace(id.namespace)
			so.Content = content

			currentObjects = append(currentObjects, *so)
			currentCount++
			currentSize += len(content)
			currentPending = append(currentPending, pendingEntry{
				pos:      [2]int{phaseIdx, objIdx},
				identity: id,
			})
		}
	}
	finalizeCurrent()

	return result, nil
}

func replaceInlineWithRefs(cod *orbac.ClusterObjectDeploymentApplyConfiguration, pack *slicePackResult) {
	if cod == nil || cod.Spec == nil || cod.Spec.Template == nil || cod.Spec.Template.Spec == nil {
		return
	}
	for phaseIdx := range cod.Spec.Template.Spec.Phases {
		for objIdx := range cod.Spec.Template.Spec.Phases[phaseIdx].Objects {
			ref, ok := pack.refs[[2]int{phaseIdx, objIdx}]
			if !ok {
				continue
			}
			cod.Spec.Template.Spec.Phases[phaseIdx].Objects[objIdx].Object = nil
			cod.Spec.Template.Spec.Phases[phaseIdx].Objects[objIdx].ObjectRef = ref
		}
	}
}

type objectIdentity struct {
	apiVersion string
	kind       string
	name       string
	namespace  string
}

func parseObjectIdentity(raw []byte) (objectIdentity, error) {
	var partial struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &partial); err != nil {
		return objectIdentity{}, fmt.Errorf("parsing object identity: %w", err)
	}
	if partial.APIVersion == "" || partial.Kind == "" {
		return objectIdentity{}, fmt.Errorf("object missing apiVersion or kind")
	}
	return objectIdentity{
		apiVersion: partial.APIVersion,
		kind:       partial.Kind,
		name:       partial.Metadata.Name,
		namespace:  partial.Metadata.Namespace,
	}, nil
}

func (p *slicePacker) sliceNameFromObjects(objects []orbac.SliceObjectApplyConfiguration) string {
	h := sha256.New()
	keys := make([]string, 0, len(objects))
	contentByKey := make(map[string][]byte, len(objects))
	for i := range objects {
		key := fmt.Sprintf("%s/%s/%s/%s",
			deref(objects[i].APIVersion),
			deref(objects[i].Kind),
			deref(objects[i].Namespace),
			deref(objects[i].Name),
		)
		keys = append(keys, key)
		contentByKey[key] = objects[i].Content
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write(contentByKey[k])
	}
	return fmt.Sprintf("%s-%x", p.codName, h.Sum(nil)[:8])
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func gzipData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
