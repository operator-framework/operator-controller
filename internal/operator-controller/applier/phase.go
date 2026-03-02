package applier

import (
	"cmp"
	"slices"

	"k8s.io/apimachinery/pkg/runtime/schema"

	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
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
	PhaseNamespaces     Phase = "namespaces"
	PhasePolicies       Phase = "policies"
	PhaseIdentity       Phase = "identity"
	PhaseConfiguration  Phase = "configuration"
	PhaseStorage        Phase = "storage"
	PhaseCRDs           Phase = "crds"
	PhaseRoles          Phase = "roles"
	PhaseBindings       Phase = "bindings"
	PhaseInfrastructure Phase = "infrastructure"
	PhaseDeploy         Phase = "deploy"
	PhaseScaling        Phase = "scaling"
	PhasePublish        Phase = "publish"
	PhaseAdmission      Phase = "admission"
)

// Well known phases ordered.
var defaultPhaseOrder = []Phase{
	PhaseNamespaces,
	PhasePolicies,
	PhaseIdentity,
	PhaseConfiguration,
	PhaseStorage,
	PhaseCRDs,
	PhaseRoles,
	PhaseBindings,
	PhaseInfrastructure,
	PhaseDeploy,
	PhaseScaling,
	PhasePublish,
	PhaseAdmission,
}

// Note: OLMv1 currently only supports registry+v1 content. The registry+v1 format only supports a limited
// set of object kinds defined in:
// https://github.com/operator-framework/operator-registry/blob/f410a396abe01dbe6a46b6d90d34bdd844306388/pkg/lib/bundle/supported_resources.go
// The phase mapping considers all allowable registry+v1 bundle format resource with the following changes:
// - ClusterServiceVersion is replaced by the resources it describes: Deployment, Cluster/Role/Binding, ServiceAccount, ValidatingWebhookConfiguration, etc.
// - Certificate and Issuer from cert-manager are added since OLMv1 uses cert-manager for webhook service certificate by default
var (
	// This will be populated from `phaseGKMap` in an init func!
	gkPhaseMap = map[schema.GroupKind]Phase{}
	phaseGKMap = map[Phase][]schema.GroupKind{
		PhaseNamespaces: {
			{Kind: "Namespace"},
		},

		PhasePolicies: {
			{Kind: "NetworkPolicy", Group: "networking.k8s.io"},
			{Kind: "PodDisruptionBudget", Group: "policy"},
			{Kind: "PriorityClass", Group: "scheduling.k8s.io"},
		},

		PhaseIdentity: {
			{Kind: "ServiceAccount"},
		},

		PhaseConfiguration: {
			{Kind: "Secret"},
			{Kind: "ConfigMap"},
		},

		PhaseStorage: {
			{Kind: "PersistentVolume"},
			{Kind: "PersistentVolumeClaim"},
			{Kind: "StorageClass", Group: "storage.k8s.io"},
		},

		PhaseCRDs: {
			{Kind: "CustomResourceDefinition", Group: "apiextensions.k8s.io"},
		},

		PhaseRoles: {
			{Kind: "ClusterRole", Group: "rbac.authorization.k8s.io"},
			{Kind: "Role", Group: "rbac.authorization.k8s.io"},
		},

		PhaseBindings: {
			{Kind: "ClusterRoleBinding", Group: "rbac.authorization.k8s.io"},
			{Kind: "RoleBinding", Group: "rbac.authorization.k8s.io"},
		},

		PhaseInfrastructure: {
			{Kind: "Service"},
			{Kind: "Issuer", Group: "cert-manager.io"},
		},

		PhaseDeploy: {
			{Kind: "Certificate", Group: "cert-manager.io"},
			{Kind: "Deployment", Group: "apps"},
		},

		PhaseScaling: {
			{Kind: "VerticalPodAutoscaler", Group: "autoscaling.k8s.io"},
		},

		PhasePublish: {
			{Kind: "PrometheusRule", Group: "monitoring.coreos.com"},
			{Kind: "ServiceMonitor", Group: "monitoring.coreos.com"},
			{Kind: "PodMonitor", Group: "monitoring.coreos.com"},
			{Kind: "Ingress", Group: "networking.k8s.io"},
			{Kind: "Route", Group: "route.openshift.io"},
			{Kind: "ConsoleYAMLSample", Group: "console.openshift.io"},
			{Kind: "ConsoleQuickStart", Group: "console.openshift.io"},
			{Kind: "ConsoleCLIDownload", Group: "console.openshift.io"},
			{Kind: "ConsoleLink", Group: "console.openshift.io"},
			{Kind: "ConsolePlugin", Group: "console.openshift.io"},
		},

		PhaseAdmission: {
			{Kind: "ValidatingWebhookConfiguration", Group: "admissionregistration.k8s.io"},
			{Kind: "MutatingWebhookConfiguration", Group: "admissionregistration.k8s.io"},
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

// Sort objects within the phase deterministically by Group, Version, Kind, Namespace, Name
// to ensure consistent ordering regardless of input order. This is critical for
// Helm-to-Boxcutter migration where the same resources may come from different sources
// (Helm release manifest vs bundle manifest) and need to produce identical phases.
func compareClusterExtensionRevisionObjectApplyConfigurations(a, b ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration) int {
	aGVK := a.Object.GroupVersionKind()
	bGVK := b.Object.GroupVersionKind()

	return cmp.Or(
		cmp.Compare(aGVK.Group, bGVK.Group),
		cmp.Compare(aGVK.Version, bGVK.Version),
		cmp.Compare(aGVK.Kind, bGVK.Kind),
		cmp.Compare(a.Object.GetNamespace(), b.Object.GetNamespace()),
		cmp.Compare(a.Object.GetName(), b.Object.GetName()),
	)
}

// PhaseSort takes an unsorted list of objects and organizes them into sorted phases.
// Each phase will be applied in order according to DefaultPhaseOrder. Objects
// within a single phase are applied simultaneously.
func PhaseSort(unsortedObjs []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration) []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration {
	phasesSorted := make([]*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration, 0)
	phaseMap := make(map[Phase][]ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration)

	for _, obj := range unsortedObjs {
		phase := determinePhase(obj.Object.GroupVersionKind().GroupKind())
		phaseMap[phase] = append(phaseMap[phase], obj)
	}

	for _, phaseName := range defaultPhaseOrder {
		if objs, ok := phaseMap[phaseName]; ok {
			// Sort objects within the phase deterministically
			slices.SortFunc(objs, compareClusterExtensionRevisionObjectApplyConfigurations)

			// Convert to pointers for WithObjects
			objPtrs := make([]*ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration, len(objs))
			for i := range objs {
				objPtrs[i] = &objs[i]
			}
			phasesSorted = append(phasesSorted, ocv1ac.ClusterExtensionRevisionPhase().
				WithName(string(phaseName)).
				WithObjects(objPtrs...))
		}
	}

	return phasesSorted
}
