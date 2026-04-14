package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing/fstest"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	bundlecsv "github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	bundlefs "github.com/operator-framework/operator-controller/internal/testing/bundle/fs"
)

// BundleOption configures what manifests a bundle contains.
type BundleOption func(*bundleConfig)

type bundleProperty struct {
	propertyType string
	value        string
}

type bundleConfig struct {
	hasCRD                  bool
	hasDeployment           bool
	hasConfigMap            bool
	badContainerImage       bool
	properties              []bundleProperty
	installModes            []v1alpha1.InstallModeType // if nil, defaults to AllNamespaces + SingleNamespace
	largeCRDFieldCount      int                        // if > 0, generate a CRD with this many fields
	staticBundleDir         string                     // if set, read bundle from this directory (no parameterization)
	clusterRegistryOverride string                     // if set, use this host in the FBC image ref instead of the default
}

// bundleSpec is the resolved bundle: version + file map ready for crane.Image().
type bundleSpec struct {
	version                 string
	files                   map[string][]byte
	clusterRegistryOverride string // if set, use this host in the FBC image ref
}

// WithCRD includes a CRD in the bundle.
func WithCRD() BundleOption {
	return func(c *bundleConfig) { c.hasCRD = true }
}

// WithDeployment includes a deployment (with CSV, script ConfigMap, and NetworkPolicy) in the bundle.
func WithDeployment() BundleOption {
	return func(c *bundleConfig) { c.hasDeployment = true }
}

// WithConfigMap includes an additional test ConfigMap in the bundle.
func WithConfigMap() BundleOption {
	return func(c *bundleConfig) { c.hasConfigMap = true }
}

// WithInstallMode sets the supported install modes for the bundle's CSV.
// If not called, defaults to AllNamespaces + SingleNamespace.
// Mode names: AllNamespaces, SingleNamespace, OwnNamespace, MultiNamespace.
func WithInstallMode(modes ...v1alpha1.InstallModeType) BundleOption {
	return func(c *bundleConfig) {
		c.installModes = append(c.installModes, modes...)
	}
}

// WithLargeCRD includes a CRD with many fields to test large bundle handling.
func WithLargeCRD(fieldCount int) BundleOption {
	return func(c *bundleConfig) {
		c.hasCRD = true
		c.largeCRDFieldCount = fieldCount
	}
}

// WithClusterRegistry overrides the cluster registry hostname used in the FBC image
// reference for this bundle. The bundle is still pushed to the main local registry,
// but the FBC entry tells the cluster to pull from the specified hostname.
// This is used for testing registry mirroring via registries.conf.
func WithClusterRegistry(host string) BundleOption {
	return func(c *bundleConfig) {
		c.clusterRegistryOverride = host
	}
}

// StaticBundleDir reads pre-built bundle manifests from the given directory.
// The bundle content is NOT parameterized — resource names remain as-is.
// Use this for bundles with real operator binaries that can't have their
// CRD names changed (e.g. webhook-operator).
func StaticBundleDir(dir string) BundleOption {
	return func(c *bundleConfig) {
		c.staticBundleDir = dir
	}
}

// WithBundleProperty adds a property to the bundle's metadata/properties.yaml.
func WithBundleProperty(propertyType, value string) BundleOption {
	return func(c *bundleConfig) {
		c.properties = append(c.properties, bundleProperty{propertyType: propertyType, value: value})
	}
}

// BadImage produces a bundle with CRD and deployment but uses "wrong/image" as
// the container image, causing ImagePullBackOff at runtime.
func BadImage() BundleOption {
	return func(c *bundleConfig) {
		c.hasCRD = true
		c.hasDeployment = true
		c.badContainerImage = true
	}
}

// buildBundle generates the manifest files for a single bundle using the existing
// CSV builder and bundle FS builder. All resource names embed scenarioID for uniqueness.
func buildBundle(scenarioID, packageName, version string, opts []BundleOption) (bundleSpec, error) {
	cfg := &bundleConfig{}
	for _, o := range opts {
		o(cfg)
	}

	// Static bundle: read files from disk without parameterization
	if cfg.staticBundleDir != "" {
		files, err := readBundleDir(cfg.staticBundleDir)
		if err != nil {
			return bundleSpec{}, fmt.Errorf("failed to read static bundle dir %s: %w", cfg.staticBundleDir, err)
		}
		return bundleSpec{
			version:                 version,
			files:                   files,
			clusterRegistryOverride: cfg.clusterRegistryOverride,
		}, nil
	}

	crdGroup := fmt.Sprintf("e2e-%s.e2e.operatorframework.io", scenarioID)
	crdPlural := fmt.Sprintf("e2e-%stests", scenarioID)
	crdKind := "E2ETest"
	crdName := fmt.Sprintf("%s.%s", crdPlural, crdGroup)
	deploymentName := fmt.Sprintf("test-operator-%s", scenarioID)
	saName := fmt.Sprintf("bundle-manager-%s", scenarioID)
	scriptCMName := fmt.Sprintf("httpd-script-%s", scenarioID)

	containerImage := "busybox:1.36"
	if cfg.badContainerImage {
		containerImage = "wrong/image"
	}

	// Build the CSV using the existing CSV builder
	var installModes []v1alpha1.InstallModeType
	if len(cfg.installModes) == 0 {
		installModes = []v1alpha1.InstallModeType{v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace}
	} else {
		installModes = append(installModes, cfg.installModes...)
	}
	csvBuilder := bundlecsv.Builder().
		WithName(fmt.Sprintf("%s.v%s", packageName, version)).
		WithInstallModeSupportFor(installModes...)

	if cfg.hasCRD {
		csvBuilder = csvBuilder.WithOwnedCRDs(v1alpha1.CRDDescription{
			Name:        crdName,
			Kind:        crdKind,
			Version:     "v1",
			DisplayName: crdKind,
			Description: "E2E Test Resource",
		})
	}

	if cfg.hasDeployment {
		csvBuilder = csvBuilder.
			WithStrategyDeploymentSpecs(buildDeploymentSpec(
				deploymentName, scenarioID, version, saName, scriptCMName, containerImage,
			)).
			WithPermissions(v1alpha1.StrategyDeploymentPermissions{
				ServiceAccountName: saName,
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"configmaps", "serviceaccounts"},
						Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
					},
					{
						APIGroups: []string{"networking.k8s.io"},
						Resources: []string{"networkpolicies"},
						Verbs:     []string{"get", "list", "create", "update", "delete"},
					},
					{
						APIGroups: []string{"coordination.k8s.io"},
						Resources: []string{"leases"},
						Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
					},
					{
						APIGroups: []string{""},
						Resources: []string{"events"},
						Verbs:     []string{"create", "patch"},
					},
				},
			}).
			WithClusterPermissions(v1alpha1.StrategyDeploymentPermissions{
				ServiceAccountName: saName,
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{"authentication.k8s.io"},
						Resources: []string{"tokenreviews"},
						Verbs:     []string{"create"},
					},
					{
						APIGroups: []string{"authorization.k8s.io"},
						Resources: []string{"subjectaccessreviews"},
						Verbs:     []string{"create"},
					},
				},
			})
	}

	// Build the bundle FS using the existing FS builder
	fsBuilder := bundlefs.Builder().
		WithPackageName(packageName).
		WithCSV(csvBuilder.Build())

	// Add script ConfigMap for the deployment
	if cfg.hasDeployment {
		fsBuilder = fsBuilder.
			WithBundleResource("script-configmap.yaml", &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: scriptCMName,
				},
				Data: map[string]string{
					"httpd.sh": "#!/bin/sh\necho true > /var/www/started\necho true > /var/www/ready\necho true > /var/www/live\nexec httpd -f -h /var/www -p 80\n",
				},
			}).
			WithBundleResource("networkpolicy.yaml", &networkingv1.NetworkPolicy{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "NetworkPolicy",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-network-policy", deploymentName),
				},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{},
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				},
			})
	}

	// Add test ConfigMap if requested
	if cfg.hasConfigMap {
		testCMName := fmt.Sprintf("test-configmap-%s", scenarioID)
		fsBuilder = fsBuilder.WithBundleResource("bundle-configmap.yaml", &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: testCMName,
			},
			Data: map[string]string{
				"version": version,
				"name":    testCMName,
			},
		})
	}

	// Add CRD as a bundle resource
	if cfg.hasCRD {
		fsBuilder = fsBuilder.WithBundleResource("crd.yaml", buildCRD(crdName, crdGroup, crdKind, crdPlural, scenarioID, cfg.largeCRDFieldCount))
	}

	// Add bundle properties
	for _, p := range cfg.properties {
		fsBuilder = fsBuilder.WithBundleProperty(p.propertyType, p.value)
	}

	bundleFS := fsBuilder.Build()
	return bundleSpec{
		version:                 version,
		files:                   mapFSToFileMap(bundleFS),
		clusterRegistryOverride: cfg.clusterRegistryOverride,
	}, nil
}

// buildDeploymentSpec creates the StrategyDeploymentSpec for the CSV.
func buildDeploymentSpec(
	deploymentName, scenarioID, version, saName, scriptCMName, containerImage string,
) v1alpha1.StrategyDeploymentSpec {
	return v1alpha1.StrategyDeploymentSpec{
		Name: deploymentName,
		Label: labels.Set{
			"app.kubernetes.io/name":      deploymentName,
			"app.kubernetes.io/version":   version,
			"app.kubernetes.io/component": "controller",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": scenarioID,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": scenarioID,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:            saName,
					TerminationGracePeriodSeconds: ptr.To(int64(0)),
					Volumes: []corev1.Volume{
						{
							Name: "scripts",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: scriptCMName,
									},
									DefaultMode: ptr.To(int32(0755)),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "busybox-httpd-container",
							Image:   containerImage,
							Command: []string{"/scripts/httpd.sh"},
							Ports: []corev1.ContainerPort{
								{ContainerPort: 80},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "scripts",
									MountPath: "/scripts",
									ReadOnly:  true,
								},
							},
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/started",
										Port: intstr.FromInt32(80),
									},
								},
								FailureThreshold: 30,
								PeriodSeconds:    10,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/live",
										Port: intstr.FromInt32(80),
									},
								},
								FailureThreshold: 1,
								PeriodSeconds:    2,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ready",
										Port: intstr.FromInt32(80),
									},
								},
								InitialDelaySeconds: 1,
								PeriodSeconds:       1,
							},
						},
					},
				},
			},
		},
	}
}

// buildCRD creates a CustomResourceDefinition as a client.Object for use with WithBundleResource.
// If largeCRDFieldCount > 0, the CRD spec will contain that many string fields with long descriptions.
func buildCRD(name, group, kind, plural, scenarioID string, largeCRDFieldCount int) *apiextensionsv1.CustomResourceDefinition {
	specProperties := map[string]apiextensionsv1.JSONSchemaProps{
		"testField": {Type: "string"},
	}
	if largeCRDFieldCount > 0 {
		longDescBase := "This field provides configuration for the large CRD test operator. It is used to validate that the OLM installation pipeline correctly handles bundles containing large Custom Resource Definitions. "
		longDesc := strings.Repeat(longDescBase, 20) // ~4KB per field, matching the original test fixture
		for i := range largeCRDFieldCount {
			specProperties[fmt.Sprintf("field%04d", i)] = apiextensionsv1.JSONSchemaProps{
				Type:        "string",
				Description: longDesc,
			}
		}
	}

	return &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     kind,
				ListKind: kind + "List",
				Plural:   plural,
				Singular: "e2e-" + scenarioID + "test",
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type:       "object",
									Properties: specProperties,
								},
							},
						},
					},
				},
			},
		},
	}
}

// mapFSToFileMap converts an fstest.MapFS to map[string][]byte for use with crane.Image().
func mapFSToFileMap(mfs fstest.MapFS) map[string][]byte {
	files := make(map[string][]byte, len(mfs))
	for path, file := range mfs {
		files[path] = file.Data
	}
	return files
}

// readBundleDir reads all files from a bundle directory into a map[string][]byte.
func readBundleDir(dir string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = data
		return nil
	})
	return files, err
}
