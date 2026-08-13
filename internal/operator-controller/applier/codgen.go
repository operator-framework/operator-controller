package applier

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"

	"github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	orbv1alpha1 "github.com/joelanford/orb-operator/api/v1alpha1"
	orbac "github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/shared/util/cache"
)

const maxObjectsPerPhase = 50

type CODGenerator interface {
	GenerateCOD(
		ctx context.Context,
		bundleFS fs.FS,
		ext *ocv1.ClusterExtension,
		objectLabels, revisionAnnotations map[string]string,
	) (*orbac.ClusterObjectDeploymentApplyConfiguration, error)
}

type RegistryV1CODGenerator struct {
	ManifestProvider ManifestProvider
	Scheme           *runtime.Scheme
}

func (g *RegistryV1CODGenerator) GenerateCOD(
	ctx context.Context,
	bundleFS fs.FS,
	ext *ocv1.ClusterExtension,
	objectLabels, revisionAnnotations map[string]string,
) (*orbac.ClusterObjectDeploymentApplyConfiguration, error) {
	objs, err := g.ManifestProvider.Get(bundleFS, ext)
	if err != nil {
		return nil, fmt.Errorf("getting manifests: %w", err)
	}

	templateAnnotations := make(map[string]string, len(revisionAnnotations)+1)
	for k, v := range revisionAnnotations {
		templateAnnotations[k] = v
	}
	if bundleFS != nil {
		bundleAnnotations, err := getBundleAnnotations(bundleFS)
		if err != nil {
			return nil, fmt.Errorf("getting bundle annotations: %w", err)
		}
		if v, ok := bundleAnnotations[source.PropertyOLMProperties]; ok {
			if _, exists := templateAnnotations[source.PropertyOLMProperties]; !exists {
				templateAnnotations[source.PropertyOLMProperties] = v
			}
		}
	}

	phases, err := g.buildPhases(ctx, objs, objectLabels)
	if err != nil {
		return nil, fmt.Errorf("building phases: %w", err)
	}

	codSpec := orbac.ClusterObjectDeploymentSpec().
		WithTemplate(orbac.ClusterObjectDeploymentTemplate().
			WithMetadata(orbac.ClusterObjectDeploymentTemplateMetadata().
				WithAnnotations(templateAnnotations).
				WithLabels(objectLabels)).
			WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
				WithCollisionProtection(orbv1alpha1.CollisionProtectionPrevent).
				WithPhases(phases...)))
	if p := ext.Spec.ProgressDeadlineMinutes; p > 0 {
		codSpec.WithProgressDeadlineMinutes(p)
	}

	// Set a controller ownerReference to the ClusterExtension so the COD (and,
	// via propagation, its ClusterObjectSlices) is garbage-collected when the
	// ClusterExtension is deleted, and so owner-based watches enqueue the CE.
	gvk, err := apiutil.GVKForObject(ext, g.Scheme)
	if err != nil {
		return nil, fmt.Errorf("getting GVK for owner: %w", err)
	}

	return orbac.ClusterObjectDeployment(ext.Name).
		WithLabels(objectLabels).
		WithOwnerReferences(metav1ac.OwnerReference().
			WithAPIVersion(gvk.GroupVersion().String()).
			WithKind(gvk.Kind).
			WithName(ext.Name).
			WithUID(ext.UID).
			WithBlockOwnerDeletion(true).
			WithController(true)).
		WithSpec(codSpec), nil
}

func (g *RegistryV1CODGenerator) buildPhases(ctx context.Context, objs []client.Object, objectLabels map[string]string) ([]*orbac.PhaseApplyConfiguration, error) {
	phaseMap := make(map[Phase][]orbac.PhaseObjectApplyConfiguration)
	for _, obj := range objs {
		gvk, err := apiutil.GVKForObject(obj, g.Scheme)
		if err != nil {
			return nil, fmt.Errorf("getting GVK for object %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}

		obj.SetLabels(mergeStringMaps(obj.GetLabels(), objectLabels))

		phaseObj, err := toPhaseObject(ctx, obj, gvk)
		if err != nil {
			return nil, fmt.Errorf("converting object %s %s/%s: %w", gvk, obj.GetNamespace(), obj.GetName(), err)
		}

		phase := determinePhase(gvk.GroupKind())
		phaseMap[phase] = append(phaseMap[phase], *phaseObj)
	}

	var phases []*orbac.PhaseApplyConfiguration
	for _, phaseName := range defaultPhaseOrder {
		phaseObjs, ok := phaseMap[phaseName]
		if !ok {
			continue
		}
		slices.SortFunc(phaseObjs, comparePhaseObjectApplyConfigurations)

		chunks := slices.Collect(slices.Chunk(phaseObjs, maxObjectsPerPhase))
		multiChunk := len(chunks) > 1
		for chunkIdx, chunk := range chunks {
			name := string(phaseName)
			if multiChunk {
				name = fmt.Sprintf("%s-%d", name, chunkIdx+1)
			}

			ptrs := make([]*orbac.PhaseObjectApplyConfiguration, len(chunk))
			for i := range chunk {
				ptrs[i] = &chunk[i]
			}
			phases = append(phases, orbac.Phase().
				WithName(name).
				WithObjects(ptrs...))
		}
	}
	return phases, nil
}

func toPhaseObject(ctx context.Context, obj client.Object, gvk schema.GroupVersionKind) (*orbac.PhaseObjectApplyConfiguration, error) {
	unstrObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("converting to unstructured: %w", err)
	}
	unstr := unstructured.Unstructured{Object: unstrObj}
	unstr.SetGroupVersionKind(gvk)

	if err := cache.ApplyStripAnnotationsTransform(&unstr); err != nil {
		return nil, fmt.Errorf("stripping annotations: %w", err)
	}
	sanitizedUnstructured(ctx, &unstr)

	raw, err := json.Marshal(unstr.Object)
	if err != nil {
		return nil, fmt.Errorf("marshaling to JSON: %w", err)
	}

	phaseObj := orbac.PhaseObject().
		WithObject(runtime.RawExtension{Raw: raw})

	if assertions := assertionsForGVK(gvk.GroupKind()); len(assertions) > 0 {
		phaseObj.WithAssertions(assertions...)
	}

	return phaseObj, nil
}

func comparePhaseObjectApplyConfigurations(a, b orbac.PhaseObjectApplyConfiguration) int {
	aObj := parseRawGVKNameNs(a.Object)
	bObj := parseRawGVKNameNs(b.Object)
	return cmp.Or(
		cmp.Compare(aObj.group, bObj.group),
		cmp.Compare(aObj.version, bObj.version),
		cmp.Compare(aObj.kind, bObj.kind),
		cmp.Compare(aObj.namespace, bObj.namespace),
		cmp.Compare(aObj.name, bObj.name),
	)
}

type rawObjIdentity struct {
	group, version, kind, namespace, name string
}

func parseRawGVKNameNs(raw *runtime.RawExtension) rawObjIdentity {
	if raw == nil {
		return rawObjIdentity{}
	}
	var obj unstructured.Unstructured
	if err := json.Unmarshal(raw.Raw, &obj.Object); err != nil {
		return rawObjIdentity{}
	}
	gvk := obj.GroupVersionKind()
	return rawObjIdentity{
		group:     gvk.Group,
		version:   gvk.Version,
		kind:      gvk.Kind,
		namespace: obj.GetNamespace(),
		name:      obj.GetName(),
	}
}

var gkAssertions = map[schema.GroupKind][]*orbac.AssertionApplyConfiguration{
	{Group: apiextensions.GroupName, Kind: "CustomResourceDefinition"}: {
		orbac.Assertion().WithConditionEqual(
			orbac.ConditionEqualAssertion().
				WithType(string(apiextensions.Established)).
				WithStatus(string(corev1.ConditionTrue))),
	},
	{Group: certmanager.GroupName, Kind: "Certificate"}: {
		orbac.Assertion().WithConditionEqual(
			orbac.ConditionEqualAssertion().
				WithType("Ready").
				WithStatus("True")),
	},
	{Group: certmanager.GroupName, Kind: "Issuer"}: {
		orbac.Assertion().WithConditionEqual(
			orbac.ConditionEqualAssertion().
				WithType("Ready").
				WithStatus("True")),
	},
	{Kind: "Namespace"}: {
		orbac.Assertion().WithFieldValue(
			orbac.FieldValueAssertion().
				WithFieldPath("status.phase").
				WithValue(string(corev1.NamespaceActive))),
	},
	{Group: appsv1.GroupName, Kind: "Deployment"}: {
		orbac.Assertion().WithFieldsEqual(
			orbac.FieldsEqualAssertion().
				WithFieldA("status.updatedReplicas").
				WithFieldB("status.replicas")),
		orbac.Assertion().WithConditionEqual(
			orbac.ConditionEqualAssertion().
				WithType(string(appsv1.DeploymentAvailable)).
				WithStatus(string(corev1.ConditionTrue))),
	},
}

func assertionsForGVK(gk schema.GroupKind) []*orbac.AssertionApplyConfiguration {
	return gkAssertions[gk]
}
