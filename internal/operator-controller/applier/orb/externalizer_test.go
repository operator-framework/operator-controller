package orb

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	orbac "github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
)

func rawObject(apiVersion, kind, name, namespace string) runtime.RawExtension {
	obj := map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name": name,
		},
	}
	if namespace != "" {
		obj["metadata"].(map[string]interface{})["namespace"] = namespace
	}
	data, _ := json.Marshal(obj)
	return runtime.RawExtension{Raw: data}
}

func rawObjectWithData(name string, extraBytes int) runtime.RawExtension {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": "ns1",
		},
		"data": map[string]interface{}{
			"payload": strings.Repeat("x", extraBytes),
		},
	}
	data, _ := json.Marshal(obj)
	return runtime.RawExtension{Raw: data}
}

func TestPack_SinglePhaseMultipleObjects(t *testing.T) {
	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("deploy").WithObjects(
			orbac.PhaseObject().WithObject(rawObject("v1", "ConfigMap", "cm1", "ns1")),
			orbac.PhaseObject().WithObject(rawObject("apps/v1", "Deployment", "d1", "ns1")),
			orbac.PhaseObject().WithObject(rawObject("v1", "Service", "svc1", "ns1")),
		),
	}

	p := &slicePacker{codName: "test"}
	result, err := p.pack(phases)
	require.NoError(t, err)

	require.Len(t, result.slices, 1)
	assert.Equal(t, int32(3), *result.slices[0].Count)
	assert.Len(t, result.slices[0].Objects, 3)
	assert.Len(t, result.refs, 3)

	for _, ref := range result.refs {
		assert.True(t, strings.HasPrefix(*ref.SliceName, "test-"))
	}
}

func TestPack_MultiPhaseObjectRefs(t *testing.T) {
	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("crds").WithObjects(
			orbac.PhaseObject().WithObject(rawObject("apiextensions.k8s.io/v1", "CustomResourceDefinition", "things.example.com", "")),
		),
		*orbac.Phase().WithName("deploy").WithObjects(
			orbac.PhaseObject().WithObject(rawObject("apps/v1", "Deployment", "d1", "ns1")),
		),
	}

	p := &slicePacker{codName: "test"}
	result, err := p.pack(phases)
	require.NoError(t, err)

	ref0 := result.refs[[2]int{0, 0}]
	require.NotNil(t, ref0)
	assert.Equal(t, "apiextensions.k8s.io/v1", *ref0.APIVersion)
	assert.Equal(t, "CustomResourceDefinition", *ref0.Kind)
	assert.Equal(t, "things.example.com", *ref0.Name)
	assert.Empty(t, *ref0.Namespace)

	ref1 := result.refs[[2]int{1, 0}]
	require.NotNil(t, ref1)
	assert.Equal(t, "apps/v1", *ref1.APIVersion)
	assert.Equal(t, "Deployment", *ref1.Kind)
	assert.Equal(t, "d1", *ref1.Name)
	assert.Equal(t, "ns1", *ref1.Namespace)
}

func TestPack_SkipsNilAndEmptyObjects(t *testing.T) {
	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("deploy").WithObjects(
			orbac.PhaseObject(),
			orbac.PhaseObject().WithObject(runtime.RawExtension{}),
			orbac.PhaseObject().WithObject(rawObject("v1", "ConfigMap", "cm1", "ns1")),
		),
	}

	p := &slicePacker{codName: "test"}
	result, err := p.pack(phases)
	require.NoError(t, err)

	require.Len(t, result.slices, 1)
	assert.Len(t, result.slices[0].Objects, 1)
	assert.Len(t, result.refs, 1)

	_, has0 := result.refs[[2]int{0, 0}]
	assert.False(t, has0)
	_, has1 := result.refs[[2]int{0, 1}]
	assert.False(t, has1)
	_, has2 := result.refs[[2]int{0, 2}]
	assert.True(t, has2)
}

func TestPack_EmptyPhases(t *testing.T) {
	p := &slicePacker{codName: "test"}
	result, err := p.pack(nil)
	require.NoError(t, err)
	assert.Empty(t, result.slices)
	assert.Empty(t, result.refs)
}

func TestPack_OversizedObjectAfterCompression(t *testing.T) {
	incompressible := func(size int) string {
		b := make([]byte, 0, size)
		h := sha256.Sum256([]byte("oversized"))
		for len(b) < size {
			b = append(b, h[:]...)
			h = sha256.Sum256(h[:])
		}
		return base64.RawStdEncoding.EncodeToString(b[:size])
	}
	data, _ := json.Marshal(map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": "huge"},
		"data":       map[string]interface{}{"payload": incompressible(2 * maxDataSize)},
	})

	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("deploy").WithObjects(
			orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: data}),
		),
	}

	p := &slicePacker{codName: "test"}
	_, err := p.pack(phases)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum data size")
}

func TestPack_MissingAPIVersionOrKind(t *testing.T) {
	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("bad").WithObjects(
			orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: []byte(`{"metadata":{"name":"x"}}`)}),
		),
	}

	p := &slicePacker{codName: "test"}
	_, err := p.pack(phases)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `phase "bad" object 0`)
	assert.Contains(t, err.Error(), "missing apiVersion or kind")
}

func TestPack_InvalidJSON(t *testing.T) {
	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("bad").WithObjects(
			orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: []byte(`{not valid`)}),
		),
	}

	p := &slicePacker{codName: "test"}
	_, err := p.pack(phases)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `phase "bad" object 0`)
}

func TestPack_SplitByCount(t *testing.T) {
	objects := make([]*orbac.PhaseObjectApplyConfiguration, 0, 300)
	for i := range 300 {
		objects = append(objects, orbac.PhaseObject().WithObject(
			rawObject("v1", "ConfigMap", fmt.Sprintf("cm-%d", i), "ns1"),
		))
	}
	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("deploy").WithObjects(objects...),
	}

	p := &slicePacker{codName: "test"}
	result, err := p.pack(phases)
	require.NoError(t, err)

	require.Len(t, result.slices, 2)
	assert.Len(t, result.slices[0].Objects, 256)
	assert.Len(t, result.slices[1].Objects, 44)
	assert.Equal(t, int32(256), *result.slices[0].Count)
	assert.Equal(t, int32(44), *result.slices[1].Count)
	assert.Len(t, result.refs, 300)
}

func TestPack_SplitBySize(t *testing.T) {
	incompressible := func(size, seed int) string {
		b := make([]byte, 0, size)
		h := sha256.Sum256([]byte(fmt.Sprintf("seed-%d", seed)))
		for len(b) < size {
			b = append(b, []byte(fmt.Sprintf("%x", h))...)
			h = sha256.Sum256(h[:])
		}
		return string(b[:size])
	}
	objects := make([]*orbac.PhaseObjectApplyConfiguration, 0, 20)
	for i := range 20 {
		data, _ := json.Marshal(map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": fmt.Sprintf("big-%d", i), "namespace": "ns1"},
			"data":       map[string]interface{}{"payload": incompressible(100*1024, i)},
		})
		objects = append(objects, orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: data}))
	}
	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("deploy").WithObjects(objects...),
	}

	p := &slicePacker{codName: "test"}
	result, err := p.pack(phases)
	require.NoError(t, err)
	assert.Greater(t, len(result.slices), 1)
	assert.Len(t, result.refs, 20)
}

func TestPack_ContentAlwaysGzipped(t *testing.T) {
	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("deploy").WithObjects(
			orbac.PhaseObject().WithObject(rawObject("v1", "ConfigMap", "cm1", "ns1")),
		),
	}

	p := &slicePacker{codName: "test"}
	result, err := p.pack(phases)
	require.NoError(t, err)

	require.Len(t, result.slices, 1)
	require.Len(t, result.slices[0].Objects, 1)
	content := result.slices[0].Objects[0].Content
	require.GreaterOrEqual(t, len(content), 2)
	assert.Equal(t, byte(0x1f), content[0])
	assert.Equal(t, byte(0x8b), content[1])
}

func TestPack_DeterministicNames(t *testing.T) {
	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("deploy").WithObjects(
			orbac.PhaseObject().WithObject(rawObject("v1", "ConfigMap", "cm1", "ns1")),
			orbac.PhaseObject().WithObject(rawObject("v1", "ConfigMap", "cm2", "ns1")),
		),
	}

	p := &slicePacker{codName: "test"}
	r1, err := p.pack(phases)
	require.NoError(t, err)
	r2, err := p.pack(phases)
	require.NoError(t, err)

	require.Len(t, r1.slices, 1)
	require.Len(t, r2.slices, 1)
	assert.Equal(t, *r1.slices[0].GetName(), *r2.slices[0].GetName())
}

func TestPack_DistinctIdentitiesNotDeduplicated(t *testing.T) {
	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("deploy").WithObjects(
			orbac.PhaseObject().WithObject(rawObject("v1", "ConfigMap", "cm-a", "ns1")),
			orbac.PhaseObject().WithObject(rawObject("v1", "ConfigMap", "cm-b", "ns1")),
		),
	}

	p := &slicePacker{codName: "test"}
	result, err := p.pack(phases)
	require.NoError(t, err)
	require.Len(t, result.slices, 1)
	assert.Len(t, result.slices[0].Objects, 2)
}

func TestPack_ClusterScopedObject(t *testing.T) {
	phases := []orbac.PhaseApplyConfiguration{
		*orbac.Phase().WithName("crds").WithObjects(
			orbac.PhaseObject().WithObject(rawObject("apiextensions.k8s.io/v1", "CustomResourceDefinition", "things.example.com", "")),
		),
	}

	p := &slicePacker{codName: "test"}
	result, err := p.pack(phases)
	require.NoError(t, err)

	ref := result.refs[[2]int{0, 0}]
	require.NotNil(t, ref)
	assert.Empty(t, *ref.Namespace)
}

func TestExternalize_SmallCOD_Unchanged(t *testing.T) {
	cod := orbac.ClusterObjectDeployment("small").
		WithSpec(orbac.ClusterObjectDeploymentSpec().
			WithTemplate(orbac.ClusterObjectDeploymentTemplate().
				WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
					WithPhases(
						orbac.Phase().WithName("deploy").WithObjects(
							orbac.PhaseObject().WithObject(rawObject("v1", "ConfigMap", "cm1", "ns1")),
						),
					))))

	result, slices, err := ExternalizeCOD(cod)
	require.NoError(t, err)
	assert.Same(t, cod, result)
	assert.Nil(t, slices)
	assert.NotNil(t, result.Spec.Template.Spec.Phases[0].Objects[0].Object)
}

func TestExternalize_LargeCOD_ProducesSlices(t *testing.T) {
	phases := make([]*orbac.PhaseApplyConfiguration, 0, 10)
	for i := range 10 {
		objects := make([]*orbac.PhaseObjectApplyConfiguration, 0, 5)
		for j := range 5 {
			obj := orbac.PhaseObject().WithObject(
				rawObjectWithData(fmt.Sprintf("cm-%d-%d", i, j), 20*1024),
			)
			objects = append(objects, obj)
		}
		phases = append(phases, orbac.Phase().WithName(fmt.Sprintf("phase-%d", i)).WithObjects(objects...))
	}
	cod := orbac.ClusterObjectDeployment("large").
		WithSpec(orbac.ClusterObjectDeploymentSpec().
			WithTemplate(orbac.ClusterObjectDeploymentTemplate().
				WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
					WithPhases(phases...))))

	result, slices, err := ExternalizeCOD(cod)
	require.NoError(t, err)
	assert.Same(t, cod, result)
	assert.NotEmpty(t, slices)

	for _, phase := range result.Spec.Template.Spec.Phases {
		for _, obj := range phase.Objects {
			if obj.Object == nil && obj.ObjectRef != nil {
				assert.True(t, strings.HasPrefix(*obj.ObjectRef.SliceName, "large-"))
			}
		}
	}
}

func TestExternalize_PropagatesCODLabelsToSlices(t *testing.T) {
	cod := orbac.ClusterObjectDeployment("labeled-ext").
		WithLabels(map[string]string{
			"olm.operatorframework.io/owner-kind": "ClusterExtension",
			"olm.operatorframework.io/owner-name": "my-ext",
		}).
		WithSpec(orbac.ClusterObjectDeploymentSpec().
			WithTemplate(orbac.ClusterObjectDeploymentTemplate().
				WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
					WithPhases(
						orbac.Phase().WithName("deploy").WithObjects(
							orbac.PhaseObject().WithObject(rawObjectWithData("cm1", 500*1024)),
							orbac.PhaseObject().WithObject(rawObjectWithData("cm2", 500*1024)),
						),
					))))

	_, slices, err := ExternalizeCOD(cod)
	require.NoError(t, err)
	require.NotEmpty(t, slices)

	for _, s := range slices {
		require.NotNil(t, s.Labels)
		assert.Equal(t, "ClusterExtension", s.Labels["olm.operatorframework.io/owner-kind"])
		assert.Equal(t, "my-ext", s.Labels["olm.operatorframework.io/owner-name"])
	}
}

func TestExternalize_PropagatesCODOwnerReferencesToSlices(t *testing.T) {
	cod := orbac.ClusterObjectDeployment("owned-ext").
		WithOwnerReferences(metav1ac.OwnerReference().
			WithAPIVersion("olm.operatorframework.io/v1").
			WithKind("ClusterExtension").
			WithName("my-ext").
			WithUID("test-uid").
			WithController(true).
			WithBlockOwnerDeletion(true)).
		WithSpec(orbac.ClusterObjectDeploymentSpec().
			WithTemplate(orbac.ClusterObjectDeploymentTemplate().
				WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
					WithPhases(
						orbac.Phase().WithName("deploy").WithObjects(
							orbac.PhaseObject().WithObject(rawObjectWithData("cm1", 500*1024)),
							orbac.PhaseObject().WithObject(rawObjectWithData("cm2", 500*1024)),
						),
					))))

	_, slices, err := ExternalizeCOD(cod)
	require.NoError(t, err)
	require.NotEmpty(t, slices)

	for _, s := range slices {
		require.Len(t, s.OwnerReferences, 1)
		ref := s.OwnerReferences[0]
		assert.Equal(t, "ClusterExtension", *ref.Kind)
		assert.Equal(t, "my-ext", *ref.Name)
		assert.Equal(t, "test-uid", string(*ref.UID))
		require.NotNil(t, ref.Controller)
		assert.True(t, *ref.Controller)
	}
}

func TestExternalize_NoOwnerReferencesWhenCODHasNone(t *testing.T) {
	cod := orbac.ClusterObjectDeployment("unowned-ext").
		WithSpec(orbac.ClusterObjectDeploymentSpec().
			WithTemplate(orbac.ClusterObjectDeploymentTemplate().
				WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
					WithPhases(
						orbac.Phase().WithName("deploy").WithObjects(
							orbac.PhaseObject().WithObject(rawObjectWithData("cm1", 500*1024)),
							orbac.PhaseObject().WithObject(rawObjectWithData("cm2", 500*1024)),
						),
					))))

	_, slices, err := ExternalizeCOD(cod)
	require.NoError(t, err)
	require.NotEmpty(t, slices)

	for _, s := range slices {
		assert.Empty(t, s.OwnerReferences)
	}
}

func TestExternalize_NoLabelsWhenCODHasNone(t *testing.T) {
	cod := orbac.ClusterObjectDeployment("unlabeled-ext").
		WithSpec(orbac.ClusterObjectDeploymentSpec().
			WithTemplate(orbac.ClusterObjectDeploymentTemplate().
				WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
					WithPhases(
						orbac.Phase().WithName("deploy").WithObjects(
							orbac.PhaseObject().WithObject(rawObjectWithData("cm1", 500*1024)),
							orbac.PhaseObject().WithObject(rawObjectWithData("cm2", 500*1024)),
						),
					))))

	// Clear the name-derived labels: ClusterObjectDeployment() only sets
	// name/kind/apiVersion, not labels, so Labels should be nil here.
	_, slices, err := ExternalizeCOD(cod)
	require.NoError(t, err)
	require.NotEmpty(t, slices)

	for _, s := range slices {
		assert.Empty(t, s.Labels)
	}
}

func TestExternalize_MissingIdentity(t *testing.T) {
	cod := orbac.ClusterObjectDeployment("bad-ext").
		WithSpec(orbac.ClusterObjectDeploymentSpec().
			WithTemplate(orbac.ClusterObjectDeploymentTemplate().
				WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
					WithPhases(
						orbac.Phase().WithName("bad").WithObjects(
							orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: []byte(`{"metadata":{"name":"x"}}`)}),
							orbac.PhaseObject().WithObject(
								rawObjectWithData("pad1", 500*1024),
							),
							orbac.PhaseObject().WithObject(
								rawObjectWithData("pad2", 500*1024),
							),
						),
					))))

	_, _, err := ExternalizeCOD(cod)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `phase "bad" object 0`)
	assert.Contains(t, err.Error(), "missing apiVersion or kind")
}

func TestExternalizeCOS_SmallCOS_Unchanged(t *testing.T) {
	cos := orbac.ClusterObjectSet("small").
		WithSpec(orbac.ClusterObjectSetSpec().
			WithGroup("small").
			WithPhases(
				orbac.Phase().WithName("migrate").WithObjects(
					orbac.PhaseObject().WithObject(rawObject("v1", "ConfigMap", "cm1", "ns1")),
				),
			))

	result, slices, err := ExternalizeCOS(cos)
	require.NoError(t, err)
	assert.Same(t, cos, result)
	assert.Nil(t, slices)
	assert.NotNil(t, result.Spec.Phases[0].Objects[0].Object)
}

func TestExternalizeCOS_LargeCOS_ProducesSlices(t *testing.T) {
	objects := make([]*orbac.PhaseObjectApplyConfiguration, 0, 5)
	for j := range 5 {
		objects = append(objects, orbac.PhaseObject().WithObject(
			rawObjectWithData(fmt.Sprintf("cm-%d", j), 500*1024),
		))
	}
	cos := orbac.ClusterObjectSet("large").
		WithLabels(map[string]string{
			"olm.operatorframework.io/owner-name": "my-ext",
		}).
		WithOwnerReferences(metav1ac.OwnerReference().
			WithAPIVersion("olm.operatorframework.io/v1").
			WithKind("ClusterExtension").
			WithName("my-ext").
			WithUID("test-uid").
			WithBlockOwnerDeletion(true)).
		WithSpec(orbac.ClusterObjectSetSpec().
			WithGroup("large").
			WithPhases(
				orbac.Phase().WithName("migrate").WithObjects(objects...),
			))

	result, slices, err := ExternalizeCOS(cos)
	require.NoError(t, err)
	assert.Same(t, cos, result)
	require.NotEmpty(t, slices)

	// Phases rewritten to objectRefs, inline objects cleared.
	sawRef := false
	for _, phase := range result.Spec.Phases {
		for _, obj := range phase.Objects {
			if obj.ObjectRef != nil {
				sawRef = true
				assert.True(t, strings.HasPrefix(*obj.ObjectRef.SliceName, "large-"))
				assert.Nil(t, obj.Object)
			}
		}
	}
	assert.True(t, sawRef)

	// Labels and owner references propagated to slices; the CE owner reference
	// remains non-controller.
	for _, s := range slices {
		assert.Equal(t, "my-ext", s.Labels["olm.operatorframework.io/owner-name"])
		require.Len(t, s.OwnerReferences, 1)
		assert.Nil(t, s.OwnerReferences[0].Controller)
	}
}

func TestExternalize_DeterministicNaming(t *testing.T) {
	makeCOD := func() *orbac.ClusterObjectDeploymentApplyConfiguration {
		return orbac.ClusterObjectDeployment("det-ext").
			WithSpec(orbac.ClusterObjectDeploymentSpec().
				WithTemplate(orbac.ClusterObjectDeploymentTemplate().
					WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
						WithPhases(
							orbac.Phase().WithName("deploy").WithObjects(
								orbac.PhaseObject().WithObject(
									rawObjectWithData("cm1", 500*1024),
								),
								orbac.PhaseObject().WithObject(
									rawObjectWithData("cm2", 500*1024),
								),
							),
						))))
	}

	_, slices1, err := ExternalizeCOD(makeCOD())
	require.NoError(t, err)

	_, slices2, err := ExternalizeCOD(makeCOD())
	require.NoError(t, err)

	require.Len(t, slices1, len(slices2))
	for i := range slices1 {
		assert.Equal(t, *slices1[i].GetName(), *slices2[i].GetName())
	}
}
