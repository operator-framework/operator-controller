package migration

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

// possibleResourceGVKs lists all resource GVKs that may be part of an OLMv0 operator installation.
var possibleResourceGVKs = []schema.GroupVersionKind{
	{Group: "", Version: "v1", Kind: "Namespace"},
	{Group: "", Version: "v1", Kind: "Secret"},
	{Group: "", Version: "v1", Kind: "ConfigMap"},
	{Group: "", Version: "v1", Kind: "ServiceAccount"},
	{Group: "", Version: "v1", Kind: "Service"},
	{Group: "apps", Version: "v1", Kind: "Deployment"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
	{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"},
	{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingWebhookConfiguration"},
	{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "MutatingWebhookConfiguration"},
	{Group: "monitoring.coreos.com", Version: "v1", Kind: "PrometheusRule"},
	{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"},
	{Group: "monitoring.coreos.com", Version: "v1", Kind: "PodMonitor"},
	{Group: "policy", Version: "v1", Kind: "PodDisruptionBudget"},
	{Group: "scheduling.k8s.io", Version: "v1", Kind: "PriorityClass"},
	{Group: "networking.k8s.io", Version: "v1", Kind: "NetworkPolicy"},
	{Group: "autoscaling.k8s.io", Version: "v1", Kind: "VerticalPodAutoscaler"},
	{Group: "console.openshift.io", Version: "v1", Kind: "ConsoleYAMLSample"},
	{Group: "console.openshift.io", Version: "v1", Kind: "ConsoleQuickStart"},
	{Group: "console.openshift.io", Version: "v1", Kind: "ConsoleCLIDownload"},
	{Group: "console.openshift.io", Version: "v1", Kind: "ConsoleLink"},
	{Group: "console.openshift.io", Version: "v1", Kind: "ConsolePlugin"},
}

// clusterScopedKinds is the set of kinds that are cluster-scoped (no namespace in lookups).
var clusterScopedKinds = map[string]bool{
	"Namespace":                      true,
	"ClusterRole":                    true,
	"ClusterRoleBinding":             true,
	"CustomResourceDefinition":       true,
	"PriorityClass":                  true,
	"ConsoleYAMLSample":              true,
	"ConsoleQuickStart":              true,
	"ConsoleCLIDownload":             true,
	"ConsoleLink":                    true,
	"ConsolePlugin":                  true,
	"ValidatingWebhookConfiguration": true,
	"MutatingWebhookConfiguration":   true,
}

// GetCSVAndInstallPlan retrieves the Subscription, CSV, and InstallPlan.
func (m *Migrator) GetCSVAndInstallPlan(ctx context.Context, opts Options) (*operatorsv1alpha1.Subscription, *operatorsv1alpha1.ClusterServiceVersion, *operatorsv1alpha1.InstallPlan, error) {
	var sub operatorsv1alpha1.Subscription
	if err := m.Client.Get(ctx, types.NamespacedName{
		Name:      opts.SubscriptionName,
		Namespace: opts.SubscriptionNamespace,
	}, &sub); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get Subscription: %w", err)
	}

	csvName := sub.Status.InstalledCSV
	if csvName == "" {
		return nil, nil, nil, fmt.Errorf("subscription has no installedCSV")
	}

	var csv operatorsv1alpha1.ClusterServiceVersion
	if err := m.Client.Get(ctx, types.NamespacedName{
		Name:      csvName,
		Namespace: opts.SubscriptionNamespace,
	}, &csv); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get CSV %s: %w", csvName, err)
	}

	var ip *operatorsv1alpha1.InstallPlan
	if sub.Status.InstallPlanRef != nil {
		ip = &operatorsv1alpha1.InstallPlan{}
		if err := m.Client.Get(ctx, types.NamespacedName{
			Name:      sub.Status.InstallPlanRef.Name,
			Namespace: sub.Status.InstallPlanRef.Namespace,
		}, ip); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get InstallPlan %s: %w", sub.Status.InstallPlanRef.Name, err)
		}
	}

	return &sub, &csv, ip, nil
}

// GetBundleInfo extracts bundle metadata from the InstallPlan's bundleLookups.
func (m *Migrator) GetBundleInfo(ctx context.Context, opts Options, csv *operatorsv1alpha1.ClusterServiceVersion, ip *operatorsv1alpha1.InstallPlan) (*MigrationInfo, error) {
	var sub operatorsv1alpha1.Subscription
	if err := m.Client.Get(ctx, types.NamespacedName{
		Name:      opts.SubscriptionName,
		Namespace: opts.SubscriptionNamespace,
	}, &sub); err != nil {
		return nil, fmt.Errorf("failed to get Subscription: %w", err)
	}

	info := &MigrationInfo{
		PackageName:    sub.Spec.Package,
		Channel:        sub.Spec.Channel,
		ManualApproval: sub.Spec.InstallPlanApproval == operatorsv1alpha1.ApprovalManual,
		CatalogSourceRef: types.NamespacedName{
			Name:      sub.Spec.CatalogSource,
			Namespace: sub.Spec.CatalogSourceNamespace,
		},
	}

	// Extract version and bundle name from CSV
	info.BundleName = csv.Name
	info.Version = parseCSVVersion(csv)

	// Extract bundle image from InstallPlan bundleLookups
	if ip != nil {
		for _, bl := range ip.Status.BundleLookups {
			if bl.Identifier == csv.Name {
				info.BundleImage = bl.Path
				if bl.CatalogSourceRef != nil {
					info.CatalogSourceRef = types.NamespacedName{
						Name:      bl.CatalogSourceRef.Name,
						Namespace: bl.CatalogSourceRef.Namespace,
					}
				}
				break
			}
		}
	}

	return info, nil
}

// parseCSVVersion extracts the version from CSV's operatorframework.io/properties annotation.
func parseCSVVersion(csv *operatorsv1alpha1.ClusterServiceVersion) string {
	propsJSON := csv.Annotations["operatorframework.io/properties"]
	if propsJSON == "" {
		return csv.Spec.Version.String()
	}

	props, err := parseProperties(propsJSON)
	if err != nil {
		return csv.Spec.Version.String()
	}

	for _, p := range props {
		if p.Type == "olm.package" {
			var pkg struct {
				PackageName string `json:"packageName"`
				Version     string `json:"version"`
			}
			if err := json.Unmarshal(p.Value, &pkg); err == nil && pkg.Version != "" {
				return pkg.Version
			}
		}
	}
	return csv.Spec.Version.String()
}

// GetCatalogSourceImage retrieves the image reference from the CatalogSource spec.
// This returns the tag-based image (e.g., quay.io/org/catalog:latest) rather than
// the resolved digest, so that a ClusterCatalog created from it can pick up updates.
func (m *Migrator) GetCatalogSourceImage(ctx context.Context, csRef types.NamespacedName) (string, error) {
	var cs operatorsv1alpha1.CatalogSource
	if err := m.Client.Get(ctx, csRef, &cs); err != nil {
		return "", fmt.Errorf("failed to get CatalogSource %s/%s: %w", csRef.Namespace, csRef.Name, err)
	}
	if cs.Spec.Image == "" {
		return "", fmt.Errorf("CatalogSource %s/%s has no spec.image set", csRef.Namespace, csRef.Name)
	}
	return cs.Spec.Image, nil
}

// CollectResources gathers all resources belonging to the operator using multiple collection strategies.
// Per the RFC, no single OLMv0 tracking mechanism provides a complete inventory, so we combine:
//  1. Operator CR status.components.refs
//  2. CRDs by package label
//  3. Resources by olm.owner label
//  4. Resources by ownerReference
//  5. Resources from InstallPlan steps
//
// Results are deduplicated across all sources.
// olmv0OnlyKinds are resource kinds that belong to OLMv0 and should not be
// included in the ClusterExtensionRevision. They are cleaned up separately.
var olmv0OnlyKinds = map[string]bool{
	"OperatorCondition": true,
	"Operator":          true,
	"OperatorGroup":     true,
}

func (m *Migrator) CollectResources(ctx context.Context, opts Options, csv *operatorsv1alpha1.ClusterServiceVersion, ip *operatorsv1alpha1.InstallPlan, packageName string) ([]unstructured.Unstructured, error) {
	seen := make(map[string]bool)
	var collected []unstructured.Unstructured

	addIfNew := func(obj unstructured.Unstructured) {
		if olmv0OnlyKinds[obj.GetKind()] {
			return
		}
		key := resourceKey(obj)
		if !seen[key] {
			seen[key] = true
			collected = append(collected, obj)
		}
	}

	// Strategy 1: Operator CR status.components.refs (RFC Step 5)
	// Non-fatal — Operator CR may not exist on all clusters
	fromOperatorCR, _ := m.gatherResourcesFromOperatorCR(ctx, packageName, opts.SubscriptionNamespace)
	for _, obj := range fromOperatorCR {
		addIfNew(obj)
	}

	// Strategy 2: CRDs by package label
	crds, err := m.getCRDsByPackage(ctx, opts, packageName)
	if err != nil {
		return nil, fmt.Errorf("failed to collect CRDs by package: %w", err)
	}
	for _, obj := range crds {
		addIfNew(obj)
	}

	// Strategy 3: Resources by olm.owner label
	for _, obj := range m.gatherResourcesByOwnerLabel(ctx, csv.Name) {
		addIfNew(obj)
	}

	// Strategy 4: Resources by ownerReference in the subscription namespace
	for _, obj := range m.gatherResourcesByOwnerRef(ctx, opts.SubscriptionNamespace, csv) {
		addIfNew(obj)
	}

	// Strategy 5: Resources from InstallPlan steps
	if ip != nil {
		for _, obj := range m.gatherResourcesFromInstallPlan(ctx, ip, csv.Name) {
			addIfNew(obj)
		}
	}

	return collected, nil
}

func resourceKey(obj unstructured.Unstructured) string {
	return fmt.Sprintf("%s/%s/%s/%s",
		obj.GetObjectKind().GroupVersionKind().GroupKind().String(),
		obj.GetNamespace(),
		obj.GetName(),
		obj.GetAPIVersion())
}

func (m *Migrator) getCRDsByPackage(ctx context.Context, opts Options, packageName string) ([]unstructured.Unstructured, error) {
	packageLabel := fmt.Sprintf("operators.coreos.com/%s.%s", packageName, opts.SubscriptionNamespace)

	var crdList unstructured.UnstructuredList
	crdList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinitionList",
	})

	if err := m.Client.List(ctx, &crdList,
		client.MatchingLabels{
			"olm.managed": "true",
			packageLabel:  "",
		},
	); err != nil {
		return nil, err
	}
	return crdList.Items, nil
}

func (m *Migrator) gatherResourcesByOwnerLabel(ctx context.Context, csvName string) []unstructured.Unstructured {
	var result []unstructured.Unstructured

	for _, gvk := range possibleResourceGVKs {
		var list unstructured.UnstructuredList
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind + "List",
		})

		listOpts := []client.ListOption{
			client.MatchingLabels{
				"olm.managed": "true",
				"olm.owner":   csvName,
			},
		}

		if err := m.Client.List(ctx, &list, listOpts...); err != nil {
			// Skip kinds that don't exist on this cluster (e.g., monitoring CRDs not installed)
			continue
		}
		result = append(result, list.Items...)
	}
	return result
}

func (m *Migrator) gatherResourcesByOwnerRef(ctx context.Context, namespace string, csv *operatorsv1alpha1.ClusterServiceVersion) []unstructured.Unstructured {
	var result []unstructured.Unstructured

	for _, gvk := range possibleResourceGVKs {
		if clusterScopedKinds[gvk.Kind] {
			continue // ownerRefs only work within a namespace
		}

		var list unstructured.UnstructuredList
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind + "List",
		})

		if err := m.Client.List(ctx, &list, client.InNamespace(namespace)); err != nil {
			continue
		}

		for _, obj := range list.Items {
			for _, ref := range obj.GetOwnerReferences() {
				if ref.Kind == "ClusterServiceVersion" && ref.Name == csv.Name {
					result = append(result, obj)
					break
				}
			}
		}
	}
	return result
}

func (m *Migrator) gatherResourcesFromInstallPlan(ctx context.Context, ip *operatorsv1alpha1.InstallPlan, csvName string) []unstructured.Unstructured {
	var result []unstructured.Unstructured

	for _, step := range ip.Status.Plan {
		if step == nil || step.Resolving != csvName {
			continue
		}

		res := step.Resource
		if res.Kind == "ClusterServiceVersion" || res.Kind == "Subscription" || res.Kind == "InstallPlan" {
			continue
		}

		// Fetch the live object
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   res.Group,
			Version: res.Version,
			Kind:    res.Kind,
		})

		nn := types.NamespacedName{Name: res.Name}
		if !clusterScopedKinds[res.Kind] {
			nn.Namespace = ip.Namespace
		}

		if err := m.Client.Get(ctx, nn, obj); err != nil {
			// Resource may not exist yet or may have been cleaned up
			continue
		}
		result = append(result, *obj)
	}
	return result
}

// gatherResourcesFromOperatorCR collects resources from the Operator CR's status.components.refs.
// This is one of the primary collection strategies specified in RFC Step 5.
func (m *Migrator) gatherResourcesFromOperatorCR(ctx context.Context, packageName, namespace string) ([]unstructured.Unstructured, error) {
	op, err := m.GetOperatorCR(ctx, packageName, namespace)
	if err != nil {
		return nil, err
	}

	if op.Status.Components == nil {
		return nil, nil
	}

	// Kinds to skip — these are OLMv0 management resources, not operator workloads
	skipKinds := map[string]bool{
		"ClusterServiceVersion": true,
		"Subscription":          true,
		"InstallPlan":           true,
	}

	var result []unstructured.Unstructured
	for _, ref := range op.Status.Components.Refs {
		if ref.ObjectReference == nil {
			continue
		}
		if skipKinds[ref.Kind] {
			continue
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   ref.GroupVersionKind().Group,
			Version: ref.GroupVersionKind().Version,
			Kind:    ref.Kind,
		})

		nn := types.NamespacedName{Name: ref.Name}
		if ref.Namespace != "" {
			nn.Namespace = ref.Namespace
		}

		if err := m.Client.Get(ctx, nn, obj); err != nil {
			// Resource may have been deleted
			continue
		}
		result = append(result, *obj)
	}
	return result, nil
}

// GatherMigrationInfo profiles the operator and collects all migration information.
func (m *Migrator) GatherMigrationInfo(ctx context.Context, opts Options) (*MigrationInfo, error) {
	_, csv, ip, err := m.GetCSVAndInstallPlan(ctx, opts)
	if err != nil {
		return nil, err
	}

	info, err := m.GetBundleInfo(ctx, opts, csv, ip)
	if err != nil {
		return nil, err
	}

	// Get catalog source image (non-fatal — only needed if we need to create a ClusterCatalog)
	csImage, err := m.GetCatalogSourceImage(ctx, info.CatalogSourceRef)
	if err == nil {
		info.CatalogSourceImage = csImage
	}

	// Collect resources
	objects, err := m.CollectResources(ctx, opts, csv, ip, info.PackageName)
	if err != nil {
		return nil, err
	}
	info.CollectedObjects = objects

	return info, nil
}

// GetOperatorCR retrieves the Operator CR for the given package and namespace.
func (m *Migrator) GetOperatorCR(ctx context.Context, packageName, namespace string) (*operatorsv1.Operator, error) {
	operatorName := fmt.Sprintf("%s.%s", packageName, namespace)
	var op operatorsv1.Operator
	if err := m.Client.Get(ctx, types.NamespacedName{Name: operatorName}, &op); err != nil {
		return nil, err
	}
	return &op, nil
}
