package clusterobjectdeployment

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// RolloutComplete requires successful completion of the current template generation.
func RolloutComplete(cod *ocv1.ClusterObjectDeployment) bool {
	prog := apimeta.FindStatusCondition(cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeProgressing)
	return prog != nil &&
		prog.Status == metav1.ConditionFalse &&
		prog.Reason == ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed &&
		prog.ObservedGeneration == cod.Generation
}
