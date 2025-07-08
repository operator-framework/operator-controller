package generators

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	registrybundle "github.com/operator-framework/operator-registry/pkg/lib/bundle"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

const (
	tlsCrtPath = "tls.crt"
	tlsKeyPath = "tls.key"

	labelKubernetesNamespaceMetadataName = "kubernetes.io/metadata.name"
)

// volume mount name -> mount path
var certVolumeMounts = map[string]string{
	"webhook-cert": "/tmp/k8s-webhook-server/serving-certs",
}

// BundleCSVDeploymentGenerator generates all deployments defined in rv1's cluster service version (CSV). The generated
// resource aim to have parity with OLMv0 generated Deployment resources:
// - olm.targetNamespaces annotation is set with the opts.TargetNamespace value
// - the deployment spec's revision history limit is set to 1
// - merges csv annotations to the deployment template's annotations
func BundleCSVDeploymentGenerator(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}

	// collect deployments that service webhooks
	webhookDeployments := sets.Set[string]{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		webhookDeployments.Insert(wh.DeploymentName)
	}

	objs := make([]client.Object, 0, len(rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs))
	for _, depSpec := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		// Add CSV annotations to template annotations
		// See https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/install/deployment.go#L142
		annotations := util.MergeMaps(rv1.CSV.Annotations, depSpec.Spec.Template.Annotations)

		// In OLMv0 CSVs are annotated with the OperatorGroup's .spec.targetNamespaces
		// See https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/operators/olm/operatorgroup.go#L279
		// When the CSVs annotations are copied to the deployment template's annotations, they bring with it this annotation
		annotations["olm.targetNamespaces"] = strings.Join(opts.TargetNamespaces, ",")
		depSpec.Spec.Template.Annotations = annotations

		// Hardcode the deployment with RevisionHistoryLimit=1 to maintain parity with OLMv0 behaviour.
		// See https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/install/deployment.go#L177-L180
		depSpec.Spec.RevisionHistoryLimit = ptr.To(int32(1))

		deploymentResource := CreateDeploymentResource(
			depSpec.Name,
			opts.InstallNamespace,
			WithDeploymentSpec(depSpec.Spec),
			WithLabels(depSpec.Label),
		)

		secretInfo := render.CertProvisionerFor(depSpec.Name, opts).GetCertSecretInfo()
		if webhookDeployments.Has(depSpec.Name) && secretInfo != nil {
			addCertVolumesToDeployment(deploymentResource, *secretInfo)
		}

		objs = append(objs, deploymentResource)
	}
	return objs, nil
}

// BundleCSVPermissionsGenerator generates the Roles and RoleBindings based on bundle's cluster service version
// permission spec. If the bundle is being installed in AllNamespaces mode (opts.TargetNamespaces = [”])
// no resources will be generated as these permissions will be promoted to ClusterRole/Bunding(s)
func BundleCSVPermissionsGenerator(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}

	// If we're in AllNamespaces mode permissions will be treated as clusterPermissions
	if len(opts.TargetNamespaces) == 1 && opts.TargetNamespaces[0] == "" {
		return nil, nil
	}

	permissions := rv1.CSV.Spec.InstallStrategy.StrategySpec.Permissions

	objs := make([]client.Object, 0, 2*len(opts.TargetNamespaces)*len(permissions))
	for _, ns := range opts.TargetNamespaces {
		for _, permission := range permissions {
			saName := saNameOrDefault(permission.ServiceAccountName)
			name, err := opts.UniqueNameGenerator(fmt.Sprintf("%s-%s", rv1.CSV.Name, saName), permission)
			if err != nil {
				return nil, err
			}

			objs = append(objs,
				CreateRoleResource(name, ns, WithRules(permission.Rules...)),
				CreateRoleBindingResource(
					name,
					ns,
					WithSubjects(rbacv1.Subject{Kind: "ServiceAccount", Namespace: opts.InstallNamespace, Name: saName}),
					WithRoleRef(rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}),
				),
			)
		}
	}
	return objs, nil
}

// BundleCSVClusterPermissionsGenerator generates ClusterRoles and ClusterRoleBindings based on the bundle's
// cluster service version clusterPermission spec. If the bundle is being installed in AllNamespaces mode
// (opts.TargetNamespaces = [”]), the CSV's permission spec will be promoted to ClusterRole and ClusterRoleBinding
// resources. To keep parity with OLMv0, these will also include an extra rule to get, list, watch namespaces
// (see https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/operators/olm/operatorgroup.go#L539)
func BundleCSVClusterPermissionsGenerator(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}
	clusterPermissions := rv1.CSV.Spec.InstallStrategy.StrategySpec.ClusterPermissions

	// If we're in AllNamespaces mode, promote the permissions to clusterPermissions
	if len(opts.TargetNamespaces) == 1 && opts.TargetNamespaces[0] == "" {
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
		name, err := opts.UniqueNameGenerator(fmt.Sprintf("%s-%s", rv1.CSV.Name, saName), permission)
		if err != nil {
			return nil, err
		}
		objs = append(objs,
			CreateClusterRoleResource(name, WithRules(permission.Rules...)),
			CreateClusterRoleBindingResource(
				name,
				WithSubjects(rbacv1.Subject{Kind: "ServiceAccount", Namespace: opts.InstallNamespace, Name: saName}),
				WithRoleRef(rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: name}),
			),
		)
	}
	return objs, nil
}

// BundleCSVServiceAccountGenerator generates ServiceAccount resources based on the bundle's cluster service version
// permission and clusterPermission spec. One ServiceAccount resource is created / referenced service account (i.e.
// if multiple permissions reference the same service account, only one resource will be generated).
// If a clusterPermission, or permission, references an empty (”) service account, this is considered to be the
// namespace 'default' service account. A resource for the namespace 'default' service account is not generated.
func BundleCSVServiceAccountGenerator(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}
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
			objs = append(objs, CreateServiceAccountResource(serviceAccountName, opts.InstallNamespace))
		}
	}
	return objs, nil
}

// BundleCRDGenerator generates CustomResourceDefinition resources from the registry+v1 bundle. If the CRD is referenced
// by any conversion webhook defined in the bundle's cluster service version spec, the CRD is modified
// by the CertificateProvider in opts to add any annotations or modifications necessary for certificate injection.
func BundleCRDGenerator(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}

	// collect deployments to crds with conversion webhooks
	crdToDeploymentMap := map[string]v1alpha1.WebhookDescription{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if wh.Type != v1alpha1.ConversionWebhook {
			continue
		}
		for _, crdName := range wh.ConversionCRDs {
			if _, ok := crdToDeploymentMap[crdName]; ok {
				return nil, fmt.Errorf("custom resource definition '%s' is referenced by multiple conversion webhook definitions", crdName)
			}
			crdToDeploymentMap[crdName] = wh
		}
	}

	objs := make([]client.Object, 0, len(rv1.CRDs))
	for _, crd := range rv1.CRDs {
		cp := crd.DeepCopy()
		if cw, ok := crdToDeploymentMap[crd.Name]; ok {
			// OLMv0 behaviour parity
			// See https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/install/webhook.go#L232
			if crd.Spec.PreserveUnknownFields {
				return nil, fmt.Errorf("custom resource definition '%s' must have .spec.preserveUnknownFields set to false to let API Server call webhook to do the conversion", crd.Name)
			}

			// OLMv0 behaviour parity
			// https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/install/webhook.go#L242
			conversionWebhookPath := "/"
			if cw.WebhookPath != nil {
				conversionWebhookPath = *cw.WebhookPath
			}

			certProvisioner := render.CertProvisionerFor(cw.DeploymentName, opts)
			cp.Spec.Conversion = &apiextensionsv1.CustomResourceConversion{
				Strategy: apiextensionsv1.WebhookConverter,
				Webhook: &apiextensionsv1.WebhookConversion{
					ClientConfig: &apiextensionsv1.WebhookClientConfig{
						Service: &apiextensionsv1.ServiceReference{
							Namespace: opts.InstallNamespace,
							Name:      certProvisioner.ServiceName,
							Path:      &conversionWebhookPath,
							Port:      &cw.ContainerPort,
						},
					},
					ConversionReviewVersions: cw.AdmissionReviewVersions,
				},
			}

			if err := certProvisioner.InjectCABundle(cp); err != nil {
				return nil, err
			}
		}
		objs = append(objs, cp)
	}
	return objs, nil
}

// BundleAdditionalResourcesGenerator generates resources for the additional resources included in the
// bundle. If the bundle resource is namespace scoped, its namespace will be set to the value of opts.InstallNamespace.
func BundleAdditionalResourcesGenerator(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}
	objs := make([]client.Object, 0, len(rv1.Others))
	for _, res := range rv1.Others {
		supported, namespaced := registrybundle.IsSupported(res.GetKind())
		if !supported {
			return nil, fmt.Errorf("bundle contains unsupported resource: Name: %v, Kind: %v", res.GetName(), res.GetKind())
		}

		obj := res.DeepCopy()
		if namespaced {
			obj.SetNamespace(opts.InstallNamespace)
		}

		objs = append(objs, obj)
	}
	return objs, nil
}

// BundleValidatingWebhookResourceGenerator generates ValidatingAdmissionWebhookConfiguration resources based on
// the bundle's cluster service version spec. The resource is modified by the CertificateProvider in opts
// to add any annotations or modifications necessary for certificate injection.
func BundleValidatingWebhookResourceGenerator(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}

	//nolint:prealloc
	var objs []client.Object

	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if wh.Type != v1alpha1.ValidatingAdmissionWebhook {
			continue
		}
		certProvisioner := render.CertProvisionerFor(wh.DeploymentName, opts)
		webhookName := strings.TrimSuffix(wh.GenerateName, "-")
		webhookResource := CreateValidatingWebhookConfigurationResource(
			webhookName,
			opts.InstallNamespace,
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
					ClientConfig: admissionregistrationv1.WebhookClientConfig{
						Service: &admissionregistrationv1.ServiceReference{
							Namespace: opts.InstallNamespace,
							Name:      certProvisioner.ServiceName,
							Path:      wh.WebhookPath,
							Port:      &wh.ContainerPort,
						},
					},
					// It is safe to create a namespace selector even for cluster scoped CRs. A webhook
					// is never skipped for cluster scoped CRs.
					NamespaceSelector: getWebhookNamespaceSelector(opts.TargetNamespaces),
				},
			),
		)
		if err := certProvisioner.InjectCABundle(webhookResource); err != nil {
			return nil, err
		}
		objs = append(objs, webhookResource)
	}
	return objs, nil
}

// BundleMutatingWebhookResourceGenerator generates MutatingAdmissionWebhookConfiguration resources based on
// the bundle's cluster service version spec. The resource is modified by the CertificateProvider in opts
// to add any annotations or modifications necessary for certificate injection.
func BundleMutatingWebhookResourceGenerator(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}

	//nolint:prealloc
	var objs []client.Object
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if wh.Type != v1alpha1.MutatingAdmissionWebhook {
			continue
		}
		certProvisioner := render.CertProvisionerFor(wh.DeploymentName, opts)
		webhookName := strings.TrimSuffix(wh.GenerateName, "-")
		webhookResource := CreateMutatingWebhookConfigurationResource(
			webhookName,
			opts.InstallNamespace,
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
					ClientConfig: admissionregistrationv1.WebhookClientConfig{
						Service: &admissionregistrationv1.ServiceReference{
							Namespace: opts.InstallNamespace,
							Name:      certProvisioner.ServiceName,
							Path:      wh.WebhookPath,
							Port:      &wh.ContainerPort,
						},
					},
					ReinvocationPolicy: wh.ReinvocationPolicy,
					// It is safe to create a namespace selector even for cluster scoped CRs. A webhook
					// is never skipped for cluster scoped CRs.
					NamespaceSelector: getWebhookNamespaceSelector(opts.TargetNamespaces),
				},
			),
		)
		if err := certProvisioner.InjectCABundle(webhookResource); err != nil {
			return nil, err
		}
		objs = append(objs, webhookResource)
	}
	return objs, nil
}

// BundleDeploymentServiceResourceGenerator generates Service resources that support, e.g. the webhooks,
// defined in the bundle's cluster service version spec. The resource is modified by the CertificateProvider in opts
// to add any annotations or modifications necessary for certificate injection.
func BundleDeploymentServiceResourceGenerator(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}

	// collect webhook service ports
	webhookServicePortsByDeployment := map[string]sets.Set[corev1.ServicePort]{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if _, ok := webhookServicePortsByDeployment[wh.DeploymentName]; !ok {
			webhookServicePortsByDeployment[wh.DeploymentName] = sets.Set[corev1.ServicePort]{}
		}
		webhookServicePortsByDeployment[wh.DeploymentName].Insert(getWebhookServicePort(wh))
	}

	objs := make([]client.Object, 0, len(webhookServicePortsByDeployment))
	for _, deploymentSpec := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		if _, ok := webhookServicePortsByDeployment[deploymentSpec.Name]; !ok {
			continue
		}

		servicePorts := webhookServicePortsByDeployment[deploymentSpec.Name]
		ports := servicePorts.UnsortedList()
		slices.SortStableFunc(ports, func(a, b corev1.ServicePort) int {
			return cmp.Or(cmp.Compare(a.Port, b.Port), cmp.Compare(a.TargetPort.IntValue(), b.TargetPort.IntValue()))
		})

		var labelSelector map[string]string
		if deploymentSpec.Spec.Selector != nil {
			labelSelector = deploymentSpec.Spec.Selector.MatchLabels
		}

		certProvisioner := render.CertProvisionerFor(deploymentSpec.Name, opts)
		serviceResource := CreateServiceResource(
			certProvisioner.ServiceName,
			opts.InstallNamespace,
			WithServiceSpec(
				corev1.ServiceSpec{
					Ports:    ports,
					Selector: labelSelector,
				},
			),
		)

		if err := certProvisioner.InjectCABundle(serviceResource); err != nil {
			return nil, err
		}
		objs = append(objs, serviceResource)
	}

	return objs, nil
}

// CertProviderResourceGenerator generates any resources necessary for the CertificateProvider
// in opts to function correctly, e.g. Issuer or Certificate resources.
func CertProviderResourceGenerator(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
	deploymentsWithWebhooks := sets.Set[string]{}

	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		deploymentsWithWebhooks.Insert(wh.DeploymentName)
	}

	var objs []client.Object
	for _, depName := range deploymentsWithWebhooks.UnsortedList() {
		certCfg := render.CertProvisionerFor(depName, opts)
		certObjs, err := certCfg.AdditionalObjects()
		if err != nil {
			return nil, err
		}
		for _, certObj := range certObjs {
			objs = append(objs, &certObj)
		}
	}
	return objs, nil
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

func addCertVolumesToDeployment(dep *appsv1.Deployment, certSecretInfo render.CertSecretInfo) {
	volumeMountsToReplace := sets.New(slices.Collect(maps.Keys(certVolumeMounts))...)
	certVolumeMountPaths := sets.New(slices.Collect(maps.Values(certVolumeMounts))...)
	for _, c := range dep.Spec.Template.Spec.Containers {
		for _, containerVolumeMount := range c.VolumeMounts {
			if certVolumeMountPaths.Has(containerVolumeMount.MountPath) {
				volumeMountsToReplace.Insert(containerVolumeMount.Name)
			}
		}
	}

	// update pod volumes
	dep.Spec.Template.Spec.Volumes = slices.Concat(
		slices.DeleteFunc(dep.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
			return volumeMountsToReplace.Has(v.Name)
		}),
		[]corev1.Volume{
			{
				Name: "webhook-cert",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: certSecretInfo.SecretName,
						Items: []corev1.KeyToPath{
							{
								Key:  certSecretInfo.CertificateKey,
								Path: tlsCrtPath,
							},
							{
								Key:  certSecretInfo.PrivateKeyKey,
								Path: tlsKeyPath,
							},
						},
					},
				},
			},
		},
	)

	// update container volume mounts
	for i := range dep.Spec.Template.Spec.Containers {
		dep.Spec.Template.Spec.Containers[i].VolumeMounts = slices.Concat(
			slices.DeleteFunc(dep.Spec.Template.Spec.Containers[i].VolumeMounts, func(v corev1.VolumeMount) bool {
				return volumeMountsToReplace.Has(v.Name)
			}),
			func() []corev1.VolumeMount {
				volumeMounts := make([]corev1.VolumeMount, 0, len(certVolumeMounts))
				for _, name := range slices.Sorted(maps.Keys(certVolumeMounts)) {
					volumeMounts = append(volumeMounts, corev1.VolumeMount{
						Name:      name,
						MountPath: certVolumeMounts[name],
					})
				}
				return volumeMounts
			}(),
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
