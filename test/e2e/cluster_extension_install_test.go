package e2e

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	utils "github.com/operator-framework/operator-controller/internal/shared/util/testutils"
	. "github.com/operator-framework/operator-controller/test/helpers"
)

const (
	artifactName = "operator-controller-e2e"
	// pollDuration is set to 3 minutes to account for leader election time in multi-replica deployments.
	// In the worst case (previous leader crashed), leader election can take up to 163 seconds
	// (LeaseDuration: 137s + RetryPeriod: 26s). Adding buffer for reconciliation time.
	pollDuration         = 3 * time.Minute
	pollInterval         = time.Second
	testCatalogRefEnvVar = "CATALOG_IMG"
	testCatalogName      = "test-catalog"
)

func TestClusterExtensionInstallRegistry(t *testing.T) {
	type testCase struct {
		name        string
		packageName string
	}
	for _, tc := range []testCase{
		{
			name:        "no registry configuration necessary",
			packageName: "test",
		},
		{
			// NOTE: This test requires an extra configuration in /etc/containers/registries.conf, which is mounted
			// for this e2e via the ./config/components/e2e/registries-conf kustomize component as part of the e2e component.
			// The goal here is to prove that "mirrored-registry.operator-controller-e2e.svc.cluster.local:5000" is
			// mapped to the "real" registry hostname ("docker-registry.operator-controller-e2e.svc.cluster.local:5000").
			name:        "package requires mirror registry configuration in /etc/containers/registries.conf",
			packageName: "test-mirrored",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log("When a cluster extension is installed from a catalog")
			t.Log("When the extension bundle format is registry+v1")

			clusterExtension, extensionCatalog, sa, ns := TestInit(t)
			defer TestCleanup(t, extensionCatalog, clusterExtension, sa, ns)
			defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

			clusterExtension.Spec = ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: tc.packageName,
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
						},
					},
				},
				Namespace: ns.Name,
				ServiceAccount: ocv1.ServiceAccountReference{
					Name: sa.Name,
				},
			}
			t.Log("It resolves the specified package with correct bundle path")
			t.Log("By creating the ClusterExtension resource")
			require.NoError(t, c.Create(context.Background(), clusterExtension))

			t.Log("By eventually reporting a successful resolution and bundle path")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
			}, pollDuration, pollInterval)

			t.Log("By eventually reporting progressing as True")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
				cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
				require.NotNil(ct, cond)
				require.Equal(ct, metav1.ConditionTrue, cond.Status)
				require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
			}, pollDuration, pollInterval)

			t.Log("By eventually installing the package successfully")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
				cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
				require.NotNil(ct, cond)
				require.Equal(ct, metav1.ConditionTrue, cond.Status)
				require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
				require.Contains(ct, cond.Message, "Installed bundle")
				require.NotEmpty(ct, clusterExtension.Status.Install.Bundle)
			}, pollDuration, pollInterval)

			t.Log("By eventually creating the NetworkPolicy named 'test-operator-network-policy'")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				var np networkingv1.NetworkPolicy
				require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: "test-operator-network-policy", Namespace: ns.Name}, &np))
			}, pollDuration, pollInterval)

			t.Log("By verifying that no templating occurs for registry+v1 bundle manifests")
			cm := corev1.ConfigMap{}
			require.NoError(t, c.Get(context.Background(), types.NamespacedName{Namespace: ns.Name, Name: "test-configmap"}, &cm))
			require.Contains(t, cm.Annotations, "shouldNotTemplate")
			require.Contains(t, cm.Annotations["shouldNotTemplate"], "{{ $labels.namespace }}")
		})
	}
}

func TestClusterExtensionInstallRegistryDynamic(t *testing.T) {
	// NOTE: Like 'TestClusterExtensionInstallRegistry', this test also requires extra configuration in /etc/containers/registries.conf
	packageName := "dynamic"

	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When the extension bundle format is registry+v1")

	clusterExtension, extensionCatalog, sa, ns := TestInit(t)
	defer TestCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: packageName,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
				},
			},
		},
		Namespace: ns.Name,
		ServiceAccount: ocv1.ServiceAccountReference{
			Name: sa.Name,
		},
	}
	t.Log("It updates the registries.conf file contents")
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-registries-conf",
			Namespace: "olmv1-system",
		},
		Data: map[string]string{
			"registries.conf": `[[registry]]
prefix = "dynamic-registry.operator-controller-e2e.svc.cluster.local:5000"
location = "docker-registry.operator-controller-e2e.svc.cluster.local:5000"`,
		},
	}
	require.NoError(t, c.Update(context.Background(), &cm))

	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
	}, 2*time.Minute, pollInterval)

	// Give the check extra time for the pod's files to update from the configmap change.
	// The theoretical max time is the kubelet sync period of 1 minute +
	// ConfigMap cache TTL of 1 minute = 2 minutes.
	// With multi-replica deployments, add leader election time (up to 163s in worst case).
	// Total: 2 min (ConfigMap) + 2.7 min (leader election) + buffer = 5 minutes
	t.Log("By eventually reporting progressing as True")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, 5*time.Minute, pollInterval)

	t.Log("By eventually installing the package successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		require.Contains(ct, cond.Message, "Installed bundle")
		require.NotEmpty(ct, clusterExtension.Status.Install.Bundle)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallRegistryMultipleBundles(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")

	clusterExtension, extensionCatalog, sa, ns := TestInit(t)
	extraCatalogName := fmt.Sprintf("extra-test-catalog-%s", rand.String(8))
	extraCatalog, err := CreateTestCatalog(context.Background(), extraCatalogName, os.Getenv(testCatalogRefEnvVar))
	require.NoError(t, err)

	defer TestCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)
	defer func(cat *ocv1.ClusterCatalog) {
		require.NoError(t, c.Delete(context.Background(), cat))
		require.Eventually(t, func() bool {
			err := c.Get(context.Background(), types.NamespacedName{Name: cat.Name}, &ocv1.ClusterCatalog{})
			return errors.IsNotFound(err)
		}, pollDuration, pollInterval)
	}(extraCatalog)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "test",
			},
		},
		Namespace: ns.Name,
		ServiceAccount: ocv1.ServiceAccountReference{
			Name: sa.Name,
		},
	}
	t.Log("It resolves to multiple bundle paths")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually reporting a failed resolution with multiple bundles")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting Progressing == True and Reason Retrying")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
		// Catalog names are sorted alphabetically in the error message
		catalogs := []string{extensionCatalog.Name, extraCatalog.Name}
		slices.Sort(catalogs)
		expectedMessage := fmt.Sprintf("in multiple catalogs with the same priority %v", catalogs)
		require.Contains(ct, cond.Message, expectedMessage)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionBlockInstallNonSuccessorVersion(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When resolving upgrade edges")

	clusterExtension, extensionCatalog, sa, ns := TestInit(t)
	defer TestCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	t.Log("By creating an ClusterExtension at a specified version")
	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "test",
				Version:     "1.0.0",
				// No Selector since this is an exact version match
			},
		},
		Namespace: ns.Name,
		ServiceAccount: ocv1.ServiceAccountReference{
			Name: sa.Name,
		},
	}
	require.NoError(t, c.Create(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful installation")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		require.Equal(ct,
			&ocv1.ClusterExtensionInstallStatus{Bundle: ocv1.BundleMetadata{
				Name:    "test-operator.1.0.0",
				Version: "1.0.0",
			}},
			clusterExtension.Status.Install,
		)

		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("It does not allow to upgrade the ClusterExtension to a non-successor version")
	t.Log("By updating the ClusterExtension resource to a non-successor version")
	// 1.2.0 does not replace/skip/skipRange 1.0.0.
	clusterExtension.Spec.Source.Catalog.Version = "1.2.0"
	require.NoError(t, c.Update(context.Background(), clusterExtension))
	t.Log("By eventually reporting an unsatisfiable resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting Progressing == True and Reason Retrying")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
		require.Equal(ct, "error upgrading from currently installed version \"1.0.0\": no bundles found for package \"test\" matching version \"1.2.0\"", cond.Message)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionForceInstallNonSuccessorVersion(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When resolving upgrade edges")

	clusterExtension, extensionCatalog, sa, ns := TestInit(t)
	defer TestCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	t.Log("By creating an ClusterExtension at a specified version")
	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "test",
				Version:     "1.0.0",
			},
		},
		Namespace: ns.Name,
		ServiceAccount: ocv1.ServiceAccountReference{
			Name: sa.Name,
		},
	}
	require.NoError(t, c.Create(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("It allows to upgrade the ClusterExtension to a non-successor version")
	t.Log("By updating the ClusterExtension resource to a non-successor version")
	// 1.2.0 does not replace/skip/skipRange 1.0.0.
	clusterExtension.Spec.Source.Catalog.Version = "1.2.0"
	clusterExtension.Spec.Source.Catalog.UpgradeConstraintPolicy = ocv1.UpgradeConstraintPolicySelfCertified
	require.NoError(t, c.Update(context.Background(), clusterExtension))
	t.Log("By eventually reporting a satisfiable resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallSuccessorVersion(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When resolving upgrade edges")
	clusterExtension, extensionCatalog, sa, ns := TestInit(t)
	defer TestCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	t.Log("By creating an ClusterExtension at a specified version")
	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "test",
				Version:     "1.0.0",
			},
		},
		Namespace: ns.Name,
		ServiceAccount: ocv1.ServiceAccountReference{
			Name: sa.Name,
		},
	}
	require.NoError(t, c.Create(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("It does allow to upgrade the ClusterExtension to any of the successor versions within non-zero major version")
	t.Log("By updating the ClusterExtension resource by skipping versions")
	// 1.0.1 replaces 1.0.0 in the test catalog
	clusterExtension.Spec.Source.Catalog.Version = "1.0.1"
	require.NoError(t, c.Update(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallReResolvesWhenCatalogIsPatched(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("It resolves again when a catalog is patched with new ImageRef")
	clusterExtension, extensionCatalog, sa, ns := TestInit(t)
	defer TestCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "test",
				Selector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "olm.operatorframework.io/metadata.name",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{extensionCatalog.Name},
						},
					},
				},
			},
		},
		Namespace: ns.Name,
		ServiceAccount: ocv1.ServiceAccountReference{
			Name: sa.Name,
		},
	}
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)

	// patch imageRef tag on test-catalog image with v2 image
	t.Log("By patching the catalog ImageRef to point to the v2 catalog")
	updatedCatalogImage := fmt.Sprintf("%s/e2e/test-catalog:v2", os.Getenv("CLUSTER_REGISTRY_HOST"))
	err := patchTestCatalog(context.Background(), extensionCatalog.Name, updatedCatalogImage)
	require.NoError(t, err)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: extensionCatalog.Name}, extensionCatalog))
		cond := apimeta.FindStatusCondition(extensionCatalog.Status.Conditions, ocv1.TypeServing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonAvailable, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually installing the package successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		require.Contains(ct, cond.Message, "Installed bundle")
		require.Contains(ct, clusterExtension.Status.Install.Bundle.Version, "1.3.0")
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallReResolvesWhenNewCatalog(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("It resolves again when a new catalog is available")

	// Tag the image with the new tag
	var err error
	v1Image := fmt.Sprintf("%s/%s", os.Getenv("LOCAL_REGISTRY_HOST"), os.Getenv("E2E_TEST_CATALOG_V1"))
	err = crane.Tag(v1Image, latestImageTag, crane.Insecure)
	require.NoError(t, err)

	// create a test-catalog with latest image tag
	catalogName := fmt.Sprintf("test-catalog-%s", rand.String(8))
	latestCatalogImage := fmt.Sprintf("%s/e2e/test-catalog:latest", os.Getenv("CLUSTER_REGISTRY_HOST"))
	extensionCatalog, err := CreateTestCatalog(context.Background(), catalogName, latestCatalogImage)
	require.NoError(t, err)
	clusterExtensionName := fmt.Sprintf("clusterextension-%s", rand.String(8))
	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterExtensionName,
		},
	}
	ns, err := CreateNamespace(context.Background(), clusterExtensionName)
	require.NoError(t, err)
	sa, err := CreateServiceAccount(context.Background(), types.NamespacedName{Name: clusterExtensionName, Namespace: ns.Name}, clusterExtensionName)
	require.NoError(t, err)
	defer TestCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "test",
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
				},
			},
		},
		Namespace: ns.Name,
		ServiceAccount: ocv1.ServiceAccountReference{
			Name: sa.Name,
		},
	}
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)

	// update tag on test-catalog image with v2 image
	t.Log("By updating the catalog tag to point to the v2 catalog")
	v2Image := fmt.Sprintf("%s/%s", os.Getenv("LOCAL_REGISTRY_HOST"), os.Getenv("E2E_TEST_CATALOG_V2"))
	err = crane.Tag(v2Image, latestImageTag, crane.Insecure)
	require.NoError(t, err)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: extensionCatalog.Name}, extensionCatalog))
		cond := apimeta.FindStatusCondition(extensionCatalog.Status.Conditions, ocv1.TypeServing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonAvailable, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallReResolvesWhenManagedContentChanged(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("It resolves again when managed content is changed")
	clusterExtension, extensionCatalog, sa, ns := TestInit(t)
	defer TestCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "test",
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
				},
			},
		},
		Namespace: ns.Name,
		ServiceAccount: ocv1.ServiceAccountReference{
			Name: sa.Name,
		},
	}
	t.Log("It installs the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By reporting a successful installation")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		require.Contains(ct, cond.Message, "Installed bundle")
	}, pollDuration, pollInterval)

	t.Log("By deleting a managed resource")
	testConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: clusterExtension.Spec.Namespace,
		},
	}
	require.NoError(t, c.Delete(context.Background(), testConfigMap))

	t.Log("By eventually re-creating the managed resource")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: testConfigMap.Name, Namespace: testConfigMap.Namespace}, testConfigMap))
	}, pollDuration, pollInterval)
}

func TestClusterExtensionRecoversFromNoNamespaceWhenFailureFixed(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When the extension bundle format is registry+v1")

	t.Log("By not creating the Namespace and ServiceAccount")
	clusterExtension, extensionCatalog := TestInitClusterExtensionClusterCatalog(t)

	defer TestCleanup(t, extensionCatalog, clusterExtension, nil, nil)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "test",
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
				},
			},
		},
		Namespace: clusterExtension.Name,
		ServiceAccount: ocv1.ServiceAccountReference{
			Name: clusterExtension.Name,
		},
	}

	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually reporting Progressing == True with Reason Retrying")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting Installed != True")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.NotEqual(ct, metav1.ConditionTrue, cond.Status)
	}, pollDuration, pollInterval)

	t.Log("By creating the Namespace and ServiceAccount")
	sa, ns := TestInitServiceAccountNamespace(t, clusterExtension.Name)
	defer TestCleanup(t, nil, nil, sa, ns)

	// NOTE: In order to ensure predictable results we need to ensure we have a single
	// known failure with a singular fix operation. Additionally, due to the exponential
	// backoff of this eventually check we MUST ensure we do not touch the ClusterExtension
	// after creating int the Namespace and ServiceAccount.
	t.Log("By eventually installing the package successfully")
	// Use 5 minutes for recovery tests to account for exponential backoff after repeated failures
	// plus leader election time (up to 163s in worst case)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		require.Contains(ct, cond.Message, "Installed bundle")
		require.NotEmpty(ct, clusterExtension.Status.Install)
	}, 5*time.Minute, pollInterval)

	t.Log("By eventually reporting Progressing == True with Reason Success")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionRecoversFromExistingDeploymentWhenFailureFixed(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When the extension bundle format is registry+v1")

	clusterExtension, extensionCatalog, sa, ns := TestInit(t)

	defer TestCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "test",
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
				},
			},
		},
		Namespace: clusterExtension.Name,
		ServiceAccount: ocv1.ServiceAccountReference{
			Name: clusterExtension.Name,
		},
	}

	t.Log("By creating a new Deployment that can not be adopted")
	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-operator",
			Namespace: clusterExtension.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-operator"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-operator"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Command:         []string{"sleep", "1000"},
							Image:           "busybox",
							ImagePullPolicy: corev1.PullAlways,
							Name:            "busybox",
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot:             ptr.To(true),
								RunAsUser:                ptr.To(int64(1000)),
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{
										"ALL",
									},
								},
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, c.Create(context.Background(), newDeployment))

	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually reporting Progressing == True with Reason Retrying")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually failing to install the package successfully due to no adoption support")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionFalse, cond.Status)
		// TODO: We probably _should_ be testing the reason here, but helm and boxcutter applier have different reasons.
		//   Maybe we change helm to use "Absent" rather than "Failed" since the Progressing condition already captures
		//   the failure?
		//require.Equal(ct, ocv1.ReasonFailed, cond.Reason)
		require.Contains(ct, cond.Message, "No bundle installed")
	}, pollDuration, pollInterval)

	t.Log("By deleting the new Deployment")
	require.NoError(t, c.Delete(context.Background(), newDeployment))

	// NOTE: In order to ensure predictable results we need to ensure we have a single
	// known failure with a singular fix operation. Additionally, due to the exponential
	// backoff of this eventually check we MUST ensure we do not touch the ClusterExtension
	// after deleting the Deployment.
	t.Log("By eventually installing the package successfully")
	// Use 5 minutes for recovery tests to account for exponential backoff after repeated failures
	// plus leader election time (up to 163s in worst case)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		require.Contains(ct, cond.Message, "Installed bundle")
		require.NotEmpty(ct, clusterExtension.Status.Install)
	}, 5*time.Minute, pollInterval)

	t.Log("By eventually reporting Progressing == True with Reason Success")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)
}
