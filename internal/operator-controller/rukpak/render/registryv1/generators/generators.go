package generators

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	registrybundle "github.com/operator-framework/operator-registry/pkg/lib/bundle"

	"github.com/operator-framework/operator-controller/internal/operator-controller/config"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

const (
	labelKubernetesNamespaceMetadataName = "kubernetes.io/metadata.name"
)

type certVolumeConfig struct {
	Name        string
	Path        string
	TLSCertPath string
	TLSKeyPath  string
}

// certVolumeConfigs contain the expected configurations for certificate volume/mounts
// that the generated Deployment resources for bundle containing webhooks and/or apiservices
// should contain.
var certVolumeConfigs = []certVolumeConfig{
	{
		Name:        "webhook-cert",
		Path:        "/tmp/k8s-webhook-server/serving-certs",
		TLSCertPath: "tls.crt",
		TLSKeyPath:  "tls.key",
	}, {
		Name:        "apiservice-cert",
		Path:        "/apiserver.local.config/certificates",
		TLSCertPath: "apiserver.crt",
		TLSKeyPath:  "apiserver.key",
	},
}

// BundleCSVDeploymentGenerator generates all deployments defined in rv1's cluster service version (CSV). The generated
// resource aim to have parity with OLMv0 generated Deployment resources:
// - olm.targetNamespaces annotation is set with the ctx.TargetNamespace value
// - olm.operatorNamespace annotation is set with the ctx.InstallNamespace value
// - the deployment spec's revision history limit is set to 1
// - merges csv annotations to the deployment template's annotations
func BundleCSVDeploymentGenerator(ctx *render.Context) error {
	if ctx.RV1 == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	rv1 := ctx.RV1

	objs := make([]client.Object, 0, len(rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs))
	for _, depSpec := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		// Add CSV annotations to template annotations
		// See https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/install/deployment.go#L142
		annotations := util.MergeMaps(rv1.CSV.Annotations, depSpec.Spec.Template.Annotations)

		// In OLMv0, CSVs are annotated with OperatorGroup information:
		// - olm.targetNamespaces: the OperatorGroup's .spec.targetNamespaces
		// - olm.operatorNamespace: the namespace where the OperatorGroup is defined (operator's install namespace)
		// See https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/operators/olm/operatorgroup.go#L279
		// When the CSVs annotations are copied to the deployment template's annotations, they bring with it these annotations
		annotations["olm.targetNamespaces"] = strings.Join(ctx.TargetNamespaces, ",")
		annotations["olm.operatorNamespace"] = ctx.InstallNamespace
		depSpec.Spec.Template.Annotations = annotations

		// Hardcode the deployment with RevisionHistoryLimit=1 to maintain parity with OLMv0 behaviour.
		// See https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/install/deployment.go#L177-L180
		depSpec.Spec.RevisionHistoryLimit = ptr.To(int32(1))

		deploymentResource := CreateDeploymentResource(
			depSpec.Name,
			ctx.InstallNamespace,
			WithDeploymentSpec(depSpec.Spec),
			WithLabels(depSpec.Label),
		)

		// Apply deployment configuration if provided
		applyCustomConfigToDeployment(deploymentResource, ctx.DeploymentConfig)

		objs = append(objs, deploymentResource)
	}
	ctx.Objects = append(ctx.Objects, objs...)
	return nil
}

// BundleCSVPermissionsGenerator generates the Roles and RoleBindings based on bundle's cluster service version
// permission spec. If the bundle is being installed in AllNamespaces mode (ctx.TargetNamespaces = [”])
// no resources will be generated as these permissions will be promoted to ClusterRole/Bunding(s)
func BundleCSVPermissionsGenerator(ctx *render.Context) error {
	if ctx.RV1 == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	rv1 := ctx.RV1

	// If we're in AllNamespaces mode permissions will be treated as clusterPermissions
	if len(ctx.TargetNamespaces) == 1 && ctx.TargetNamespaces[0] == "" {
		return nil
	}

	permissions := rv1.CSV.Spec.InstallStrategy.StrategySpec.Permissions

	objs := make([]client.Object, 0, 2*len(ctx.TargetNamespaces)*len(permissions))
	for _, ns := range ctx.TargetNamespaces {
		for _, permission := range permissions {
			saName := saNameOrDefault(permission.ServiceAccountName)
			name := ctx.UniqueNameGenerator(fmt.Sprintf("%s-%s", rv1.CSV.Name, saName), permission)

			objs = append(objs,
				CreateRoleResource(name, ns, WithRules(permission.Rules...)),
				CreateRoleBindingResource(
					name,
					ns,
					WithSubjects(rbacv1.Subject{Kind: "ServiceAccount", Namespace: ctx.InstallNamespace, Name: saName}),
					WithRoleRef(rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}),
				),
			)
		}
	}
	ctx.Objects = append(ctx.Objects, objs...)
	return nil
}

// BundleCSVClusterPermissionsGenerator generates ClusterRoles and ClusterRoleBindings based on the bundle's
// cluster service version clusterPermission spec. If the bundle is being installed in AllNamespaces mode
// (ctx.TargetNamespaces = [”]), the CSV's permission spec will be promoted to ClusterRole and ClusterRoleBinding
// resources. To keep parity with OLMv0, these will also include an extra rule to get, list, watch namespaces
// (see https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/operators/olm/operatorgroup.go#L539)
// The reasoning for this added rule is:
//   - An operator author designing for both SingleNamespace and AllNamespaces install modes should
//     only declare the minimum permissions needed — i.e., no cluster-scoped permissions in its CSV.
//   - When OLM places that operator into a global OperatorGroup, it lifts the Roles to ClusterRoles.
//     But some operators may need to discover namespaces to function globally, which they didn't need
//     (and shouldn't have requested) in single-namespace mode.
//   - So OLM automatically appends get/list/watch on namespaces during the lift, bridging the gap
//     without requiring the operator author to over-request permissions upfront.
func BundleCSVClusterPermissionsGenerator(ctx *render.Context) error {
	if ctx.RV1 == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	rv1 := ctx.RV1
	clusterPermissions := rv1.CSV.Spec.InstallStrategy.StrategySpec.ClusterPermissions

	// If we're in AllNamespaces mode, promote the permissions to clusterPermissions
	if len(ctx.TargetNamespaces) == 1 && ctx.TargetNamespaces[0] == "" {
		for _, p := range rv1.CSV.Spec.InstallStrategy.StrategySpec.Permissions {
			p.Rules = append(p.Rules, rbacv1.PolicyRule{
				Verbs:     []string{"get", "list", "watch"},
				APIGroups: []string{corev1.GroupName},
				Resources: []string{"namespaces"},
			})
			clusterPermissions = append(clusterPermissions, p)
		}
	}

	objs := make([]client.Object, 0, 2*len(clusterPermissions))
	for _, permission := range clusterPermissions {
		saName := saNameOrDefault(permission.ServiceAccountName)
		name := ctx.UniqueNameGenerator(fmt.Sprintf("%s-%s", rv1.CSV.Name, saName), permission)
		objs = append(objs,
			CreateClusterRoleResource(name, WithRules(permission.Rules...)),
			CreateClusterRoleBindingResource(
				name,
				WithSubjects(rbacv1.Subject{Kind: "ServiceAccount", Namespace: ctx.InstallNamespace, Name: saName}),
				WithRoleRef(rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: name}),
			),
		)
	}
	ctx.Objects = append(ctx.Objects, objs...)
	return nil
}

// BundleCSVServiceAccountGenerator generates ServiceAccount resources based on the bundle's cluster service version
// permission and clusterPermission spec. One ServiceAccount resource is created / referenced service account (i.e.
// if multiple permissions reference the same service account, only one resource will be generated).
// If a clusterPermission, or permission, references an empty (”) service account, this is considered to be the
// namespace 'default' service account. A resource for the namespace 'default' service account is not generated.
func BundleCSVServiceAccountGenerator(ctx *render.Context) error {
	if ctx.RV1 == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	rv1 := ctx.RV1
	allPermissions := append(
		rv1.CSV.Spec.InstallStrategy.StrategySpec.Permissions,
		rv1.CSV.Spec.InstallStrategy.StrategySpec.ClusterPermissions...,
	)

	serviceAccountNames := sets.Set[string]{}
	for _, permission := range allPermissions {
		serviceAccountNames.Insert(saNameOrDefault(permission.ServiceAccountName))
	}

	objs := make([]client.Object, 0, len(serviceAccountNames))
	for _, serviceAccountName := range serviceAccountNames.UnsortedList() {
		// no need to generate the default service account
		if serviceAccountName != "default" {
			objs = append(objs, CreateServiceAccountResource(serviceAccountName, ctx.InstallNamespace))
		}
	}
	ctx.Objects = append(ctx.Objects, objs...)
	return nil
}

// BundleCRDGenerator generates CustomResourceDefinition resources from the registry+v1 bundle.
// Conversion-webhook wiring and certificate injection for CRDs referenced by conversion webhooks
// are applied later by the CertMutator, which owns all certificate-dependent concerns.
func BundleCRDGenerator(ctx *render.Context) error {
	if ctx.RV1 == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	rv1 := ctx.RV1

	objs := make([]client.Object, 0, len(rv1.CRDs))
	for _, crd := range rv1.CRDs {
		objs = append(objs, crd.DeepCopy())
	}
	ctx.Objects = append(ctx.Objects, objs...)
	return nil
}

// BundleAdditionalResourcesGenerator generates resources for the additional resources included in the
// bundle. If the bundle resource is namespace scoped, its namespace will be set to the value of ctx.InstallNamespace.
func BundleAdditionalResourcesGenerator(ctx *render.Context) error {
	if ctx.RV1 == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	rv1 := ctx.RV1
	objs := make([]client.Object, 0, len(rv1.Others))
	for _, res := range rv1.Others {
		supported, namespaced := registrybundle.IsSupported(res.GetKind())
		if !supported {
			return fmt.Errorf("bundle contains unsupported resource: Name: %v, Kind: %v", res.GetName(), res.GetKind())
		}

		obj := res.DeepCopy()
		if namespaced {
			obj.SetNamespace(ctx.InstallNamespace)
		}

		objs = append(objs, obj)
	}
	ctx.Objects = append(ctx.Objects, objs...)
	return nil
}

// BundleValidatingWebhookResourceGenerator generates ValidatingAdmissionWebhookConfiguration resources based on
// the bundle's cluster service version spec. The ClientConfig (service reference) and CA bundle injection
// are applied later by the CertMutator, which owns all certificate-dependent concerns.
func BundleValidatingWebhookResourceGenerator(ctx *render.Context) error {
	if ctx.RV1 == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	rv1 := ctx.RV1

	//nolint:prealloc
	var objs []client.Object

	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if wh.Type != v1alpha1.ValidatingAdmissionWebhook {
			continue
		}
		webhookName := strings.TrimSuffix(wh.GenerateName, "-")
		webhookResource := CreateValidatingWebhookConfigurationResource(
			webhookName,
			ctx.InstallNamespace,
			WithValidatingWebhooks(
				admissionregistrationv1.ValidatingWebhook{
					Name:                    webhookName,
					Rules:                   wh.Rules,
					FailurePolicy:           wh.FailurePolicy,
					MatchPolicy:             wh.MatchPolicy,
					ObjectSelector:          wh.ObjectSelector,
					SideEffects:             wh.SideEffects,
					TimeoutSeconds:          wh.TimeoutSeconds,
					AdmissionReviewVersions: wh.AdmissionReviewVersions,
					// It is safe to create a namespace selector even for cluster scoped CRs. A webhook
					// is never skipped for cluster scoped CRs.
					NamespaceSelector: getWebhookNamespaceSelector(ctx.TargetNamespaces),
				},
			),
		)
		objs = append(objs, webhookResource)
	}
	ctx.Objects = append(ctx.Objects, objs...)
	return nil
}

// BundleMutatingWebhookResourceGenerator generates MutatingAdmissionWebhookConfiguration resources based on
// the bundle's cluster service version spec. The ClientConfig (service reference) and CA bundle injection
// are applied later by the CertMutator, which owns all certificate-dependent concerns.
func BundleMutatingWebhookResourceGenerator(ctx *render.Context) error {
	if ctx.RV1 == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	rv1 := ctx.RV1

	//nolint:prealloc
	var objs []client.Object
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if wh.Type != v1alpha1.MutatingAdmissionWebhook {
			continue
		}
		webhookName := strings.TrimSuffix(wh.GenerateName, "-")
		webhookResource := CreateMutatingWebhookConfigurationResource(
			webhookName,
			ctx.InstallNamespace,
			WithMutatingWebhooks(
				admissionregistrationv1.MutatingWebhook{
					Name:                    webhookName,
					Rules:                   wh.Rules,
					FailurePolicy:           wh.FailurePolicy,
					MatchPolicy:             wh.MatchPolicy,
					ObjectSelector:          wh.ObjectSelector,
					SideEffects:             wh.SideEffects,
					TimeoutSeconds:          wh.TimeoutSeconds,
					AdmissionReviewVersions: wh.AdmissionReviewVersions,
					ReinvocationPolicy:      wh.ReinvocationPolicy,
					// It is safe to create a namespace selector even for cluster scoped CRs. A webhook
					// is never skipped for cluster scoped CRs.
					NamespaceSelector: getWebhookNamespaceSelector(ctx.TargetNamespaces),
				},
			),
		)
		objs = append(objs, webhookResource)
	}
	ctx.Objects = append(ctx.Objects, objs...)
	return nil
}

func saNameOrDefault(saName string) string {
	return cmp.Or(saName, "default")
}

func getWebhookServicePort(wh v1alpha1.WebhookDescription) corev1.ServicePort {
	containerPort := int32(443)
	if wh.ContainerPort > 0 {
		containerPort = wh.ContainerPort
	}

	targetPort := intstr.FromInt32(containerPort)
	if wh.TargetPort != nil {
		targetPort = *wh.TargetPort
	}

	return corev1.ServicePort{
		Name:       strconv.Itoa(int(containerPort)),
		Port:       containerPort,
		TargetPort: targetPort,
	}
}

// ensureCorrectDeploymentCertVolumes ensures the deployment has the correct certificate volume mounts by
// - removing all existing volumes with protected certificate volume names (i.e. webhook-cert and apiservice-cert)
// - removing all existing volumes that point to the protected certificate paths (e.g. /tmp/k8s-webhook-server/serving-certs)
// - adding the correct certificate volumes with the correct configuration
// - applying the same changes to all container volume mounts
func ensureCorrectDeploymentCertVolumes(dep *appsv1.Deployment, certSecretInfo render.CertSecretInfo) {
	// collect volumes and paths to replace
	volumesToRemove := sets.New[string]()
	protectedVolumePaths := sets.New[string]()
	certVolumes := make([]corev1.Volume, 0, len(certVolumeConfigs))
	certVolumeMounts := make([]corev1.VolumeMount, 0, len(certVolumeConfigs))
	for _, cfg := range certVolumeConfigs {
		volumesToRemove.Insert(cfg.Name)
		protectedVolumePaths.Insert(cfg.Path)
		certVolumes = append(certVolumes, corev1.Volume{
			Name: cfg.Name,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: certSecretInfo.SecretName,
					Items: []corev1.KeyToPath{
						{
							Key:  certSecretInfo.CertificateKey,
							Path: cfg.TLSCertPath,
						},
						{
							Key:  certSecretInfo.PrivateKeyKey,
							Path: cfg.TLSKeyPath,
						},
					},
				},
			},
		})
		certVolumeMounts = append(certVolumeMounts, corev1.VolumeMount{
			Name:      cfg.Name,
			MountPath: cfg.Path,
		})
	}

	for _, c := range dep.Spec.Template.Spec.Containers {
		for _, containerVolumeMount := range c.VolumeMounts {
			if protectedVolumePaths.Has(containerVolumeMount.MountPath) {
				volumesToRemove.Insert(containerVolumeMount.Name)
			}
		}
	}

	// update pod volumes
	dep.Spec.Template.Spec.Volumes = slices.Concat(
		slices.DeleteFunc(dep.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
			return volumesToRemove.Has(v.Name)
		}),
		certVolumes,
	)

	// update container volume mounts
	for i := range dep.Spec.Template.Spec.Containers {
		dep.Spec.Template.Spec.Containers[i].VolumeMounts = slices.Concat(
			slices.DeleteFunc(dep.Spec.Template.Spec.Containers[i].VolumeMounts, func(v corev1.VolumeMount) bool {
				return volumesToRemove.Has(v.Name)
			}),
			certVolumeMounts,
		)
	}
}

// getWebhookNamespaceSelector returns a label selector that matches any namespace in targetNamespaces.
// If targetNamespaces is empty, nil, or includes "" (signifying all namespaces) nil is returned.
func getWebhookNamespaceSelector(targetNamespaces []string) *metav1.LabelSelector {
	if len(targetNamespaces) > 0 && !slices.Contains(targetNamespaces, "") {
		return &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      labelKubernetesNamespaceMetadataName,
					Operator: metav1.LabelSelectorOpIn,
					Values:   targetNamespaces,
				},
			},
		}
	}
	return nil
}

// applyCustomConfigToDeployment applies the deployment configuration to all containers in the deployment.
// It follows OLMv0 behavior for applying configuration to deployments.
// See https://github.com/operator-framework/operator-lifecycle-manager/blob/v0.39.0/pkg/controller/operators/olm/overrides/inject/inject.go
func applyCustomConfigToDeployment(deployment *appsv1.Deployment, config *config.DeploymentConfig) {
	if config == nil {
		return
	}

	// Apply all configuration modifications following OLMv0 behavior
	applyEnvironmentConfig(deployment, config)
	applyEnvironmentFromConfig(deployment, config)
	applyVolumeConfig(deployment, config)
	applyVolumeMountConfig(deployment, config)
	applyTolerationsConfig(deployment, config)
	applyResourcesConfig(deployment, config)
	applyNodeSelectorConfig(deployment, config)
	applyAffinityConfig(deployment, config)
	applyAnnotationsConfig(deployment, config)
}

// applyEnvironmentConfig applies environment variables to all containers in the deployment.
// Environment variables from config override existing environment variables with the same name.
// This follows OLMv0 behavior:
// https://github.com/operator-framework/operator-lifecycle-manager/blob/v0.39.0/pkg/controller/operators/olm/overrides/inject/inject.go#L11-L27
func applyEnvironmentConfig(deployment *appsv1.Deployment, config *config.DeploymentConfig) {
	if len(config.Env) == 0 {
		return
	}

	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]

		// Create a map to track existing env var names for override behavior
		existingEnvMap := make(map[string]int)
		for idx, env := range container.Env {
			existingEnvMap[env.Name] = idx
		}

		// Apply config env vars, overriding existing ones with same name
		for _, configEnv := range config.Env {
			if existingIdx, exists := existingEnvMap[configEnv.Name]; exists {
				// Override existing env var
				container.Env[existingIdx] = configEnv
			} else {
				// Append new env var
				container.Env = append(container.Env, configEnv)
			}
		}
	}
}

// applyEnvironmentFromConfig appends EnvFrom sources to all containers in the deployment.
// Duplicate EnvFrom sources are not added.
// This follows OLMv0 behavior:
// https://github.com/operator-framework/operator-lifecycle-manager/blob/v0.39.0/pkg/controller/operators/olm/overrides/inject/inject.go#L65-L81
func applyEnvironmentFromConfig(deployment *appsv1.Deployment, config *config.DeploymentConfig) {
	if len(config.EnvFrom) == 0 {
		return
	}

	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]

		// Check for duplicates before appending
		for _, configEnvFrom := range config.EnvFrom {
			isDuplicate := false
			for _, existingEnvFrom := range container.EnvFrom {
				if reflect.DeepEqual(existingEnvFrom, configEnvFrom) {
					isDuplicate = true
					break
				}
			}
			if !isDuplicate {
				container.EnvFrom = append(container.EnvFrom, configEnvFrom)
			}
		}
	}
}

// applyVolumeConfig merges volumes into the deployment's pod spec.
// Volumes from config override existing volumes with the same name.
// This differs from OLMv0, which appends volumes without checking for duplicates:
// https://github.com/operator-framework/operator-lifecycle-manager/blob/v0.39.0/pkg/controller/operators/olm/overrides/inject/inject.go#L104-L117
func applyVolumeConfig(deployment *appsv1.Deployment, config *config.DeploymentConfig) {
	if len(config.Volumes) == 0 {
		return
	}

	existingVolMap := make(map[string]int, len(deployment.Spec.Template.Spec.Volumes))
	for i, vol := range deployment.Spec.Template.Spec.Volumes {
		existingVolMap[vol.Name] = i
	}

	for _, configVol := range config.Volumes {
		if idx, exists := existingVolMap[configVol.Name]; exists {
			deployment.Spec.Template.Spec.Volumes[idx] = configVol
		} else {
			deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, configVol)
		}
	}
}

// applyVolumeMountConfig merges volume mounts into all containers in the deployment.
// Volume mounts from config override existing volume mounts with the same name.
// This differs from OLMv0, which appends volume mounts without checking for duplicates:
// https://github.com/operator-framework/operator-lifecycle-manager/blob/v0.39.0/pkg/controller/operators/olm/overrides/inject/inject.go#L149-L165
func applyVolumeMountConfig(deployment *appsv1.Deployment, config *config.DeploymentConfig) {
	if len(config.VolumeMounts) == 0 {
		return
	}

	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]

		existingMountMap := make(map[string]int, len(container.VolumeMounts))
		for idx, mount := range container.VolumeMounts {
			existingMountMap[mount.Name] = idx
		}

		for _, configMount := range config.VolumeMounts {
			if idx, exists := existingMountMap[configMount.Name]; exists {
				container.VolumeMounts[idx] = configMount
			} else {
				container.VolumeMounts = append(container.VolumeMounts, configMount)
			}
		}
	}
}

// applyTolerationsConfig appends tolerations to the deployment's pod spec.
// Duplicate tolerations are not added.
// This follows OLMv0 behavior:
// https://github.com/operator-framework/operator-lifecycle-manager/blob/v0.39.0/pkg/controller/operators/olm/overrides/inject/inject.go#L197-L209
func applyTolerationsConfig(deployment *appsv1.Deployment, config *config.DeploymentConfig) {
	if len(config.Tolerations) == 0 {
		return
	}

	// Check for duplicates before appending
	for _, configToleration := range config.Tolerations {
		isDuplicate := false
		for _, existingToleration := range deployment.Spec.Template.Spec.Tolerations {
			if reflect.DeepEqual(existingToleration, configToleration) {
				isDuplicate = true
				break
			}
		}
		if !isDuplicate {
			deployment.Spec.Template.Spec.Tolerations = append(deployment.Spec.Template.Spec.Tolerations, configToleration)
		}
	}
}

// applyResourcesConfig applies resource requirements to all containers in the deployment.
// This completely replaces existing resource requirements.
// This follows OLMv0 behavior:
// https://github.com/operator-framework/operator-lifecycle-manager/blob/v0.39.0/pkg/controller/operators/olm/overrides/inject/inject.go#L236-L255
func applyResourcesConfig(deployment *appsv1.Deployment, config *config.DeploymentConfig) {
	if config.Resources == nil {
		return
	}

	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]
		container.Resources = *config.Resources
	}
}

// applyNodeSelectorConfig applies node selector to the deployment's pod spec.
// This completely replaces existing node selector.
// This follows OLMv0 behavior:
// https://github.com/operator-framework/operator-lifecycle-manager/blob/v0.39.0/pkg/controller/operators/olm/overrides/inject/inject.go#L257-L271
func applyNodeSelectorConfig(deployment *appsv1.Deployment, config *config.DeploymentConfig) {
	if config.NodeSelector == nil {
		return
	}

	deployment.Spec.Template.Spec.NodeSelector = config.NodeSelector
}

// isAffinityEmpty checks if an Affinity object is semantically empty.
// This accounts for YAML unmarshaling which creates empty slices instead of nil.
func isAffinityEmpty(a *corev1.Affinity) bool {
	if a == nil {
		return true
	}
	return isNodeAffinityEmpty(a.NodeAffinity) &&
		isPodAffinityEmpty(a.PodAffinity) &&
		isPodAntiAffinityEmpty(a.PodAntiAffinity)
}

// isNodeAffinityEmpty checks if a NodeAffinity object is semantically empty.
func isNodeAffinityEmpty(na *corev1.NodeAffinity) bool {
	if na == nil {
		return true
	}
	requiredEmpty := na.RequiredDuringSchedulingIgnoredDuringExecution == nil ||
		len(na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0
	return requiredEmpty && len(na.PreferredDuringSchedulingIgnoredDuringExecution) == 0
}

// isPodAffinityEmpty checks if a PodAffinity object is semantically empty.
func isPodAffinityEmpty(pa *corev1.PodAffinity) bool {
	if pa == nil {
		return true
	}
	return len(pa.RequiredDuringSchedulingIgnoredDuringExecution) == 0 &&
		len(pa.PreferredDuringSchedulingIgnoredDuringExecution) == 0
}

// isPodAntiAffinityEmpty checks if a PodAntiAffinity object is semantically empty.
func isPodAntiAffinityEmpty(paa *corev1.PodAntiAffinity) bool {
	if paa == nil {
		return true
	}
	return len(paa.RequiredDuringSchedulingIgnoredDuringExecution) == 0 &&
		len(paa.PreferredDuringSchedulingIgnoredDuringExecution) == 0
}

// applyAffinityConfig applies affinity configuration to the deployment's pod spec.
// This follows OLMv0 behavior where:
//   - nil affinity means "don't touch" the deployment's existing affinity
//   - empty affinity ({}) means "erase" the deployment's existing affinity
//   - non-nil sub-attributes override the corresponding deployment sub-attributes
//   - nil sub-attributes within a non-empty affinity are left unchanged
//   - empty sub-attributes ({}) erase the corresponding deployment sub-attributes
//
// See: https://github.com/operator-framework/operator-lifecycle-manager/blob/v0.39.0/pkg/controller/operators/olm/overrides/inject/inject.go#L273-L341
func applyAffinityConfig(deployment *appsv1.Deployment, config *config.DeploymentConfig) {
	if config.Affinity == nil {
		return
	}

	podSpec := &deployment.Spec.Template.Spec

	// Check if the config specifies an empty affinity object with all fields unset.
	// This is different from having empty sub-fields - an empty affinity {} with no fields
	// means erase everything, while affinity with empty sub-fields means selectively erase.
	configHasNoFields := config.Affinity.NodeAffinity == nil &&
		config.Affinity.PodAffinity == nil &&
		config.Affinity.PodAntiAffinity == nil

	if configHasNoFields {
		// Config is affinity: {} with no fields - erase entire affinity
		podSpec.Affinity = nil
		return
	}

	if podSpec.Affinity == nil {
		podSpec.Affinity = &corev1.Affinity{}
	}

	if config.Affinity.NodeAffinity != nil {
		if isNodeAffinityEmpty(config.Affinity.NodeAffinity) {
			podSpec.Affinity.NodeAffinity = nil
		} else {
			podSpec.Affinity.NodeAffinity = config.Affinity.NodeAffinity
		}
	}

	if config.Affinity.PodAffinity != nil {
		if isPodAffinityEmpty(config.Affinity.PodAffinity) {
			podSpec.Affinity.PodAffinity = nil
		} else {
			podSpec.Affinity.PodAffinity = config.Affinity.PodAffinity
		}
	}

	if config.Affinity.PodAntiAffinity != nil {
		if isPodAntiAffinityEmpty(config.Affinity.PodAntiAffinity) {
			podSpec.Affinity.PodAntiAffinity = nil
		} else {
			podSpec.Affinity.PodAntiAffinity = config.Affinity.PodAntiAffinity
		}
	}

	if isAffinityEmpty(podSpec.Affinity) {
		podSpec.Affinity = nil
	}
}

// applyAnnotationsConfig applies annotations to the deployment and its pod template.
// Existing deployment and pod annotations take precedence over config annotations (no override).
// This follows OLMv0 behavior:
// https://github.com/operator-framework/operator-lifecycle-manager/blob/v0.39.0/pkg/controller/operators/olm/overrides/inject/inject.go#L343-L378
func applyAnnotationsConfig(deployment *appsv1.Deployment, config *config.DeploymentConfig) {
	if len(config.Annotations) == 0 {
		return
	}

	// Apply to deployment metadata
	if deployment.Annotations == nil {
		deployment.Annotations = make(map[string]string)
	}
	for key, value := range config.Annotations {
		if _, exists := deployment.Annotations[key]; !exists {
			deployment.Annotations[key] = value
		}
	}

	// Apply to pod template metadata
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	for key, value := range config.Annotations {
		if _, exists := deployment.Spec.Template.Annotations[key]; !exists {
			deployment.Spec.Template.Annotations[key] = value
		}
	}
}
