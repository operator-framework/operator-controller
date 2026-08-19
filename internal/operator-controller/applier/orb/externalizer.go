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
	name := ""
	if n := cod.GetName(); n != nil {
		name = *n
	}
	var lbls map[string]string
	var ownerRefs []*metav1ac.OwnerReferenceApplyConfiguration
	if cod.ObjectMetaApplyConfiguration != nil {
		lbls = cod.Labels
		ownerRefs = ownerReferencePointers(cod.OwnerReferences)
	}

	result, err := externalizePhases(cod, name, cod.Spec.Template.Spec.Phases, lbls, ownerRefs)
	if err != nil {
		return nil, nil, err
	}
	if result == nil {
		return cod, nil, nil
	}

	replaceInlineWithRefs(cod.Spec.Template.Spec.Phases, result)
	return cod, result.slices, nil
}

// ExternalizeCOS is the ClusterObjectSet analogue of ExternalizeCOD. It packs a
// COS apply configuration's inline phase objects into ClusterObjectSlice apply
// configurations and rewrites the phases to objectRef entries when the COS would
// exceed the safe etcd size threshold. A small COS is returned unchanged with a
// nil slice list. Produced slices inherit the COS's labels and owner references,
// matching the COD behavior.
func ExternalizeCOS(
	cos *orbac.ClusterObjectSetApplyConfiguration,
) (*orbac.ClusterObjectSetApplyConfiguration, []*orbac.ClusterObjectSliceApplyConfiguration, error) {
	name := ""
	if n := cos.GetName(); n != nil {
		name = *n
	}
	var lbls map[string]string
	var ownerRefs []*metav1ac.OwnerReferenceApplyConfiguration
	if cos.ObjectMetaApplyConfiguration != nil {
		lbls = cos.Labels
		ownerRefs = ownerReferencePointers(cos.OwnerReferences)
	}

	var phases []orbac.PhaseApplyConfiguration
	if cos.Spec != nil {
		phases = cos.Spec.Phases
	}

	result, err := externalizePhases(cos, name, phases, lbls, ownerRefs)
	if err != nil {
		return nil, nil, err
	}
	if result == nil {
		return cos, nil, nil
	}

	replaceInlineWithRefs(cos.Spec.Phases, result)
	return cos, result.slices, nil
}

// externalizePhases is the shared core of ExternalizeCOD and ExternalizeCOS. It
// probes the marshaled size of obj; when it fits, it returns a nil result to
// signal "no externalization needed". Otherwise it packs the phases into slices
// keyed off name and propagates the given labels and owner references onto each
// slice.
func externalizePhases(
	obj any,
	name string,
	phases []orbac.PhaseApplyConfiguration,
	lbls map[string]string,
	ownerRefs []*metav1ac.OwnerReferenceApplyConfiguration,
) (*slicePackResult, error) {
	needed, err := shouldExternalize(obj)
	if err != nil {
		return nil, err
	}
	if !needed {
		return nil, nil
	}

	packer := &slicePacker{codName: name}
	result, err := packer.pack(phases)
	if err != nil {
		return nil, err
	}

	// Propagate the object's labels (owner labels, etc.) and owner references
	// onto each slice so the slices are discoverable by the same selector used
	// to find the owner, and are garbage-collected / watched alongside the
	// owner (the ClusterExtension).
	for _, slice := range result.slices {
		if len(lbls) > 0 {
			slice.WithLabels(lbls)
		}
		if len(ownerRefs) > 0 {
			slice.WithOwnerReferences(ownerRefs...)
		}
	}
	return result, nil
}

// ownerReferencePointers returns pointers to the given owner references so they
// can be copied onto each ClusterObjectSlice.
func ownerReferencePointers(refs []metav1ac.OwnerReferenceApplyConfiguration) []*metav1ac.OwnerReferenceApplyConfiguration {
	ptrs := make([]*metav1ac.OwnerReferenceApplyConfiguration, 0, len(refs))
	for i := range refs {
		ref := refs[i]
		ptrs = append(ptrs, &ref)
	}
	return ptrs
}

func shouldExternalize(obj any) (bool, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return false, fmt.Errorf("estimating object size: %w", err)
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

func replaceInlineWithRefs(phases []orbac.PhaseApplyConfiguration, pack *slicePackResult) {
	for phaseIdx := range phases {
		for objIdx := range phases[phaseIdx].Objects {
			ref, ok := pack.refs[[2]int{phaseIdx, objIdx}]
			if !ok {
				continue
			}
			phases[phaseIdx].Objects[objIdx].Object = nil
			phases[phaseIdx].Objects[objIdx].ObjectRef = ref
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
