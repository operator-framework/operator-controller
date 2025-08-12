package applier

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// The following, with modifications, is taken from:
// https://github.com/package-operator/package-operator/blob/v1.18.2/internal/packages/internal/packagekickstart/presets/phases.go
//
// Determines a phase using the objects Group Kind from a list of presets.
// Defaults to the `deploy` phase if no preset was found. Runtimes that
// depend on a custom resource to start i.e. certmanager's Certificate
// will require this.
func determinePhase(gk schema.GroupKind) Phase {
	phase, ok := gkPhaseMap[gk]
	if !ok {
		return PhaseDeploy
	}
	return phase
}

// Phase represents a well-known phase.
type Phase string

const (
	PhaseNamespaces Phase = "namespaces"
	PhasePolicies   Phase = "policies"
	PhaseRBAC       Phase = "rbac"
	PhaseCRDs       Phase = "crds"
	PhaseStorage    Phase = "storage"
	PhaseDeploy     Phase = "deploy"
	PhasePublish    Phase = "publish"
)

// Well known phases ordered.
var defaultPhaseOrder = []Phase{
	PhaseNamespaces,
	PhasePolicies,
	PhaseRBAC,
	PhaseCRDs,
	PhaseStorage,
	PhaseDeploy,
	PhasePublish,
}

var (
	// This will be populated from `phaseGKMap` in an init func!
	gkPhaseMap = map[schema.GroupKind]Phase{}
	phaseGKMap = map[Phase][]schema.GroupKind{
		PhaseNamespaces: {
			{Kind: "Namespace"},
		},

		PhasePolicies: {
			{Kind: "ResourceQuota"},
			{Kind: "LimitRange"},
			{Kind: "PriorityClass", Group: "scheduling.k8s.io"},
			{Kind: "NetworkPolicy", Group: "networking.k8s.io"},
			{Kind: "HorizontalPodAutoscaler", Group: "autoscaling"},
			{Kind: "PodDisruptionBudget", Group: "policy"},
		},

		PhaseRBAC: {
			{Kind: "ServiceAccount"},
			{Kind: "Role", Group: "rbac.authorization.k8s.io"},
			{Kind: "RoleBinding", Group: "rbac.authorization.k8s.io"},
			{Kind: "ClusterRole", Group: "rbac.authorization.k8s.io"},
			{Kind: "ClusterRoleBinding", Group: "rbac.authorization.k8s.io"},
		},

		PhaseCRDs: {
			{Kind: "CustomResourceDefinition", Group: "apiextensions.k8s.io"},
		},

		PhaseStorage: {
			{Kind: "PersistentVolume"},
			{Kind: "PersistentVolumeClaim"},
			{Kind: "StorageClass", Group: "storage.k8s.io"},
		},

		PhaseDeploy: {
			{Kind: "Deployment", Group: "apps"},
			{Kind: "DaemonSet", Group: "apps"},
			{Kind: "StatefulSet", Group: "apps"},
			{Kind: "ReplicaSet"},
			{Kind: "Pod"}, // probing complicated, may be either Completed or Available.
			{Kind: "Job", Group: "batch"},
			{Kind: "CronJob", Group: "batch"},
			{Kind: "Service"},
			{Kind: "Secret"},
			{Kind: "ConfigMap"},
		},

		PhasePublish: {
			{Kind: "Ingress", Group: "networking.k8s.io"},
			{Kind: "APIService", Group: "apiregistration.k8s.io"},
			{Kind: "Route", Group: "route.openshift.io"},
			{Kind: "MutatingWebhookConfiguration", Group: "admissionregistration.k8s.io"},
			{Kind: "ValidatingWebhookConfiguration", Group: "admissionregistration.k8s.io"},
		},
	}
)

func init() {
	for phase, gks := range phaseGKMap {
		for _, gk := range gks {
			gkPhaseMap[gk] = phase
		}
	}
}

// PhaseSort takes an unsorted list of objects and organizes them into sorted phases.
// Each phase will be applied in order according to DefaultPhaseOrder. Objects
// within a single phase are applied simultaneously.
func PhaseSort(unsortedObjs []ocv1.ClusterExtensionRevisionObject) []ocv1.ClusterExtensionRevisionPhase {
	phasesSorted := make([]ocv1.ClusterExtensionRevisionPhase, 0)
	phaseMap := make(map[Phase][]ocv1.ClusterExtensionRevisionObject, 0)

	for _, obj := range unsortedObjs {
		phase := determinePhase(obj.Object.GroupVersionKind().GroupKind())
		phaseMap[phase] = append(phaseMap[phase], obj)
	}

	for _, phaseName := range defaultPhaseOrder {
		if objs, ok := phaseMap[phaseName]; ok {
			phasesSorted = append(phasesSorted, ocv1.ClusterExtensionRevisionPhase{
				Name:    string(phaseName),
				Objects: objs,
			})
		}
	}

	return phasesSorted
}
