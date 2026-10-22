package applier

import (
	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
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
