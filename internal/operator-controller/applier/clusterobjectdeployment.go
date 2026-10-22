package applier

import (
	"context"
	"fmt"
	"io/fs"

	corev1 "k8s.io/api/core/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

// buildClusterObjectDeployment constructs a ClusterObjectDeployment applyconfiguration
// from rendered content. It mirrors the structure of buildClusterObjectSet but produces
// a COD with a template instead of directly setting COS-specific fields.
func buildClusterObjectDeployment(
	objects []ocv1ac.ClusterObjectSetObjectApplyConfiguration,
	ext *ocv1.ClusterExtension,
	annotations map[string]string,
	collisionProtection ocv1.CollisionProtection,
) *ocv1ac.ClusterObjectDeploymentApplyConfiguration {
	if annotations == nil {
		annotations = make(map[string]string)
	}

	phases := PhaseSort(objects)

	templateSpec := ocv1ac.ClusterObjectDeploymentTemplateSpec().
		WithPhases(phases...).
		WithProgressionProbes(defaultProgressionProbes...).
		WithCollisionProtection(collisionProtection)

	if p := ext.Spec.ProgressDeadlineMinutes; p > 0 {
		templateSpec.WithProgressDeadlineMinutes(p)
	}

	template := ocv1ac.ClusterObjectDeploymentTemplate().
		WithSpec(templateSpec)

	spec := ocv1ac.ClusterObjectDeploymentSpec().
		WithTemplate(template)

	return ocv1ac.ClusterObjectDeployment(ext.Name).
		WithAnnotations(annotations).
		WithLabels(map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.Name,
		}).
		WithSpec(spec)
}

// GenerateClusterObjectDeployment renders bundle content into a ClusterObjectDeployment
// with packed Secrets. It returns the COD applyconfiguration with ref'd phases and the
// content-addressed Secrets to create.
func (r *SimpleRevisionGenerator) GenerateClusterObjectDeployment(
	ctx context.Context,
	bundleFS fs.FS,
	ext *ocv1.ClusterExtension,
	objectLabels, revisionAnnotations map[string]string,
) (*ocv1ac.ClusterObjectDeploymentApplyConfiguration, []corev1.Secret, error) {
	// Get manifests from ManifestProvider
	plain, err := r.ManifestProvider.Get(bundleFS, ext)
	if err != nil {
		return nil, nil, err
	}

	if revisionAnnotations == nil {
		revisionAnnotations = map[string]string{}
	}

	// Get bundle annotations
	bundleAnnotations, err := getBundleAnnotations(bundleFS)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting bundle annotations: %w", err)
	}

	// Merge bundle annotations with revision annotations for object annotations
	objectAnnotations := map[string]string{}
	if v := revisionAnnotations[labels.BundleVersionKey]; v != "" {
		objectAnnotations[labels.BundleVersionKey] = v
	}
	if v := revisionAnnotations[labels.PackageNameKey]; v != "" {
		objectAnnotations[labels.PackageNameKey] = v
	}

	// Render objects with labels and annotations
	objs, err := r.renderObjects(ctx, plain, objectLabels, objectAnnotations)
	if err != nil {
		return nil, nil, err
	}

	// Build COD with bundle metadata annotations
	codAnnotations := make(map[string]string)
	for k, v := range revisionAnnotations {
		codAnnotations[k] = v
	}
	// Add bundle properties annotations
	for _, key := range []string{
		source.PropertyOLMProperties,
	} {
		if value, ok := bundleAnnotations[key]; ok {
			codAnnotations[key] = value
		}
	}

	// Create COD applyconfiguration (with inline objects initially)
	cod := buildClusterObjectDeployment(objs, ext, codAnnotations, ocv1.CollisionProtectionPrevent)

	// Pack objects into Secrets
	phases := extractPhasesForPacking(cod.Spec.Template.Spec.Phases)
	packer := &SecretPacker{
		OwnerName:       ext.Name,
		SystemNamespace: r.SystemNamespace,
	}
	packResult, err := packer.Pack(phases)
	if err != nil {
		return nil, nil, fmt.Errorf("packing objects into Secrets: %w", err)
	}

	// Replace inline objects with refs in COD template phases
	replaceInlineWithRefsInCOD(cod, packResult)

	return cod, packResult.Secrets, nil
}

// replaceInlineWithRefsInCOD replaces inline objects in a COD's template phases with refs.
func replaceInlineWithRefsInCOD(cod *ocv1ac.ClusterObjectDeploymentApplyConfiguration, pack *PackResult) {
	if cod.Spec == nil || cod.Spec.Template == nil || cod.Spec.Template.Spec == nil {
		return
	}
	for phaseIdx := range cod.Spec.Template.Spec.Phases {
		for objIdx := range cod.Spec.Template.Spec.Phases[phaseIdx].Objects {
			ref, ok := pack.Refs[[2]int{phaseIdx, objIdx}]
			if !ok {
				continue
			}
			cod.Spec.Template.Spec.Phases[phaseIdx].Objects[objIdx].Object = nil
			cod.Spec.Template.Spec.Phases[phaseIdx].Objects[objIdx].Ref = ocv1ac.ObjectSourceRef().
				WithName(ref.Name).
				WithNamespace(ref.Namespace).
				WithKey(ref.Key)
		}
	}
}
