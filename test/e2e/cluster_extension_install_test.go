package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/utils/env"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

const (
	artifactName = "operator-controller-e2e"
)

var pollDuration = time.Minute
var pollInterval = time.Second

func createServiceAccount(ctx context.Context, name types.NamespacedName, clusterExtensionName string) (*corev1.ServiceAccount, error) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
	}
	err := c.Create(ctx, sa)
	if err != nil {
		return nil, err
	}

	return sa, createClusterRoleAndBindingForSA(ctx, name.Name, sa, clusterExtensionName)
}

func createClusterRoleAndBindingForSA(ctx context.Context, name string, sa *corev1.ServiceAccount, clusterExtensionName string) error {
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"olm.operatorframework.io",
				},
				Resources: []string{
					"clusterextensions/finalizers",
				},
				Verbs: []string{
					"update",
				},
				ResourceNames: []string{clusterExtensionName},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"secrets", // for helm
					"services",
					"serviceaccounts",
				},
				Verbs: []string{
					"create",
					"update",
					"delete",
					"patch",
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{
					"apiextensions.k8s.io",
				},
				Resources: []string{
					"customresourcedefinitions",
				},
				Verbs: []string{
					"create",
					"update",
					"delete",
					"patch",
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{
					"apps",
				},
				Resources: []string{
					"deployments",
				},
				Verbs: []string{
					"create",
					"update",
					"delete",
					"patch",
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{
					"rbac.authorization.k8s.io",
				},
				Resources: []string{
					"clusterroles",
					"roles",
					"clusterrolebindings",
					"rolebindings",
				},
				Verbs: []string{
					"create",
					"update",
					"delete",
					"patch",
					"get",
					"list",
					"watch",
					"bind",
					"escalate",
				},
			},
		},
	}
	err := c.Create(ctx, cr)
	if err != nil {
		return err
	}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      sa.Name,
				Namespace: sa.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     name,
		},
	}
	err = c.Create(ctx, crb)
	if err != nil {
		return err
	}

	return nil
}

func testInit(t *testing.T) (*ocv1alpha1.ClusterExtension, *catalogd.ClusterCatalog, *corev1.ServiceAccount) {
	var err error
	extensionCatalog, err := createTestCatalog(context.Background(), testCatalogName, os.Getenv(testCatalogRefEnvVar))
	require.NoError(t, err)

	clusterExtensionName := fmt.Sprintf("clusterextension-%s", rand.String(8))
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterExtensionName,
		},
	}

	defaultNamespace := types.NamespacedName{
		Name:      clusterExtensionName,
		Namespace: "default",
	}

	sa, err := createServiceAccount(context.Background(), defaultNamespace, clusterExtensionName)
	require.NoError(t, err)
	return clusterExtension, extensionCatalog, sa
}

func testCleanup(t *testing.T, cat *catalogd.ClusterCatalog, clusterExtension *ocv1alpha1.ClusterExtension, sa *corev1.ServiceAccount) {
	require.NoError(t, c.Delete(context.Background(), cat))
	require.Eventually(t, func() bool {
		err := c.Get(context.Background(), types.NamespacedName{Name: cat.Name}, &catalogd.ClusterCatalog{})
		return errors.IsNotFound(err)
	}, pollDuration, pollInterval)
	require.NoError(t, c.Delete(context.Background(), clusterExtension))
	require.Eventually(t, func() bool {
		err := c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, &ocv1alpha1.ClusterExtension{})
		return errors.IsNotFound(err)
	}, pollDuration, pollInterval)
	require.NoError(t, c.Delete(context.Background(), sa))
	require.Eventually(t, func() bool {
		err := c.Get(context.Background(), types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace}, &corev1.ServiceAccount{})
		return errors.IsNotFound(err)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallRegistry(t *testing.T) {
	type testCase struct {
		name        string
		packageName string
	}
	for _, tc := range []testCase{
		{
			name:        "no registry configuration necessary",
			packageName: "prometheus",
		},
		{
			// NOTE: This test requires an extra configuration in /etc/containers/registries.conf, which is mounted
			// for this e2e via the ./config/components/registries-conf kustomize component as part of the e2e overlay.
			// The goal here is to prove that "mirrored-registry.operator-controller-e2e.svc.cluster.local:5000" is
			// mapped to the "real" registry hostname ("docker-registry.operator-controller-e2e.svc.cluster.local:5000").
			name:        "package requires mirror registry configuration in /etc/containers/registries.conf",
			packageName: "prometheus-mirrored",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log("When a cluster extension is installed from a catalog")
			t.Log("When the extension bundle format is registry+v1")

			clusterExtension, extensionCatalog, sa := testInit(t)
			defer testCleanup(t, extensionCatalog, clusterExtension, sa)
			defer getArtifactsOutput(t)

			clusterExtension.Spec = ocv1alpha1.ClusterExtensionSpec{
				Source: ocv1alpha1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1alpha1.CatalogSource{
						PackageName: tc.packageName,
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
						},
					},
				},
				Install: ocv1alpha1.ClusterExtensionInstallConfig{
					Namespace: "default",
					ServiceAccount: ocv1alpha1.ServiceAccountReference{
						Name: sa.Name,
					},
				},
			}
			t.Log("It resolves the specified package with correct bundle path")
			t.Log("By creating the ClusterExtension resource")
			require.NoError(t, c.Create(context.Background(), clusterExtension))

			t.Log("By eventually reporting a successful resolution and bundle path")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
				assert.Equal(ct,
					&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
						Name:    fmt.Sprintf("%s-operator.1.2.0", tc.packageName),
						Version: "1.2.0",
					}},
					clusterExtension.Status.Resolution,
				)
			}, pollDuration, pollInterval)

			t.Log("By eventually reporting no longer progressing")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
				cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
				if !assert.NotNil(ct, cond) {
					return
				}
				assert.Equal(ct, metav1.ConditionFalse, cond.Status)
				assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)
			}, pollDuration, pollInterval)

			t.Log("By eventually installing the package successfully")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
				cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
				if !assert.NotNil(ct, cond) {
					return
				}
				assert.Equal(ct, metav1.ConditionTrue, cond.Status)
				assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)
				assert.Contains(ct, cond.Message, "Installed bundle")
				assert.NotEmpty(ct, clusterExtension.Status.Install.Bundle)
			}, pollDuration, pollInterval)
		})
	}
}

func TestClusterExtensionInstallRegistryMultipleBundles(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")

	clusterExtension, extensionCatalog, sa := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, sa)
	defer getArtifactsOutput(t)

	clusterExtension.Spec = ocv1alpha1.ClusterExtensionSpec{
		Source: ocv1alpha1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1alpha1.CatalogSource{
				PackageName: "prometheus",
			},
		},
		Install: ocv1alpha1.ClusterExtensionInstallConfig{
			Namespace: "default",
			ServiceAccount: ocv1alpha1.ServiceAccountReference{
				Name: sa.Name,
			},
		},
	}
	t.Log("It resolves to multiple bundle paths")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually reporting a failed resolution with multiple bundles")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		assert.Nil(ct, clusterExtension.Status.Resolution)
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting Progressing == True and Reason Retrying")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonRetrying, cond.Reason)
		assert.Contains(ct, cond.Message, "in multiple catalogs with the same priority [operatorhubio test-catalog]")
	}, pollDuration, pollInterval)
}

func TestClusterExtensionBlockInstallNonSuccessorVersion(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When resolving upgrade edges")

	clusterExtension, extensionCatalog, sa := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, sa)
	defer getArtifactsOutput(t)

	t.Log("By creating an ClusterExtension at a specified version")
	clusterExtension.Spec = ocv1alpha1.ClusterExtensionSpec{
		Source: ocv1alpha1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1alpha1.CatalogSource{
				PackageName: "prometheus",
				Version:     "1.0.0",
				// No Selector since this is an exact version match
			},
		},
		Install: ocv1alpha1.ClusterExtensionInstallConfig{
			Namespace: "default",
			ServiceAccount: ocv1alpha1.ServiceAccountReference{
				Name: sa.Name,
			},
		},
	}
	require.NoError(t, c.Create(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful installation")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.1.0.0",
				Version: "1.0.0",
			}},
			clusterExtension.Status.Resolution,
		)
		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionInstallStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.1.0.0",
				Version: "1.0.0",
			}},
			clusterExtension.Status.Install,
		)

		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("It does not allow to upgrade the ClusterExtension to a non-successor version")
	t.Log("By updating the ClusterExtension resource to a non-successor version")
	// 1.2.0 does not replace/skip/skipRange 1.0.0.
	clusterExtension.Spec.Source.Catalog.Version = "1.2.0"
	require.NoError(t, c.Update(context.Background(), clusterExtension))
	t.Log("By eventually reporting an unsatisfiable resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		assert.Empty(ct, clusterExtension.Status.Resolution)
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting Progressing == True and Reason Retrying")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, ocv1alpha1.ReasonRetrying, cond.Reason)
		assert.Equal(ct, "error upgrading from currently installed version \"1.0.0\": no bundles found for package \"prometheus\" matching version \"1.2.0\"", cond.Message)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionForceInstallNonSuccessorVersion(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When resolving upgrade edges")

	clusterExtension, extensionCatalog, sa := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, sa)
	defer getArtifactsOutput(t)

	t.Log("By creating an ClusterExtension at a specified version")
	clusterExtension.Spec = ocv1alpha1.ClusterExtensionSpec{
		Source: ocv1alpha1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1alpha1.CatalogSource{
				PackageName: "prometheus",
				Version:     "1.0.0",
			},
		},
		Install: ocv1alpha1.ClusterExtensionInstallConfig{
			Namespace: "default",
			ServiceAccount: ocv1alpha1.ServiceAccountReference{
				Name: sa.Name,
			},
		},
	}
	require.NoError(t, c.Create(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)

		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.1.0.0",
				Version: "1.0.0",
			}},
			clusterExtension.Status.Resolution,
		)
	}, pollDuration, pollInterval)

	t.Log("It allows to upgrade the ClusterExtension to a non-successor version")
	t.Log("By updating the ClusterExtension resource to a non-successor version")
	// 1.2.0 does not replace/skip/skipRange 1.0.0.
	clusterExtension.Spec.Source.Catalog.Version = "1.2.0"
	clusterExtension.Spec.Source.Catalog.UpgradeConstraintPolicy = ocv1alpha1.UpgradeConstraintPolicySelfCertified
	require.NoError(t, c.Update(context.Background(), clusterExtension))
	t.Log("By eventually reporting a satisfiable resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)

		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.1.2.0",
				Version: "1.2.0",
			}},
			clusterExtension.Status.Resolution,
		)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallSuccessorVersion(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When resolving upgrade edges")
	clusterExtension, extensionCatalog, sa := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, sa)
	defer getArtifactsOutput(t)

	t.Log("By creating an ClusterExtension at a specified version")
	clusterExtension.Spec = ocv1alpha1.ClusterExtensionSpec{
		Source: ocv1alpha1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1alpha1.CatalogSource{
				PackageName: "prometheus",
				Version:     "1.0.0",
			},
		},
		Install: ocv1alpha1.ClusterExtensionInstallConfig{
			Namespace: "default",
			ServiceAccount: ocv1alpha1.ServiceAccountReference{
				Name: sa.Name,
			},
		},
	}
	require.NoError(t, c.Create(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)

		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.1.0.0",
				Version: "1.0.0",
			}},
			clusterExtension.Status.Resolution,
		)
	}, pollDuration, pollInterval)

	t.Log("It does allow to upgrade the ClusterExtension to any of the successor versions within non-zero major version")
	t.Log("By updating the ClusterExtension resource by skipping versions")
	// 1.0.1 replaces 1.0.0 in the test catalog
	clusterExtension.Spec.Source.Catalog.Version = "1.0.1"
	require.NoError(t, c.Update(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)

		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.1.0.1",
				Version: "1.0.1",
			}},
			clusterExtension.Status.Resolution,
		)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallReResolvesWhenCatalogIsPatched(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("It resolves again when a catalog is patched with new ImageRef")
	clusterExtension, extensionCatalog, sa := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, sa)
	defer getArtifactsOutput(t)

	clusterExtension.Spec = ocv1alpha1.ClusterExtensionSpec{
		Source: ocv1alpha1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1alpha1.CatalogSource{
				PackageName: "prometheus",
				Selector: metav1.LabelSelector{
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
		Install: ocv1alpha1.ClusterExtensionInstallConfig{
			Namespace: "default",
			ServiceAccount: ocv1alpha1.ServiceAccountReference{
				Name: sa.Name,
			},
		},
	}
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)

		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.1.2.0",
				Version: "1.2.0",
			}},
			clusterExtension.Status.Resolution,
		)
	}, pollDuration, pollInterval)

	// patch imageRef tag on test-catalog image with v2 image
	t.Log("By patching the catalog ImageRef to point to the v2 catalog")
	updatedCatalogImage := fmt.Sprintf("%s/e2e/test-catalog:v2", os.Getenv("LOCAL_REGISTRY_HOST"))
	err := patchTestCatalog(context.Background(), testCatalogName, updatedCatalogImage)
	require.NoError(t, err)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: extensionCatalog.Name}, extensionCatalog))
		cond := apimeta.FindStatusCondition(extensionCatalog.Status.Conditions, catalogd.TypeServing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, catalogd.ReasonAvailable, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)

		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.2.0.0",
				Version: "2.0.0",
			}},
			clusterExtension.Status.Resolution,
		)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallReResolvesWhenNewCatalog(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("It resolves again when a new catalog is available")

	// Tag the image with the new tag
	var err error
	v1Image := fmt.Sprintf("%s/%s", os.Getenv("CLUSTER_REGISTRY_HOST"), os.Getenv("E2E_TEST_CATALOG_V1"))
	err = crane.Tag(v1Image, latestImageTag, crane.Insecure)
	require.NoError(t, err)

	// create a test-catalog with latest image tag
	latestCatalogImage := fmt.Sprintf("%s/e2e/test-catalog:latest", os.Getenv("LOCAL_REGISTRY_HOST"))
	extensionCatalog, err := createTestCatalog(context.Background(), testCatalogName, latestCatalogImage)
	require.NoError(t, err)
	clusterExtensionName := fmt.Sprintf("clusterextension-%s", rand.String(8))
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterExtensionName,
		},
	}
	sa, err := createServiceAccount(context.Background(), types.NamespacedName{Name: clusterExtensionName, Namespace: "default"}, clusterExtensionName)
	require.NoError(t, err)
	defer testCleanup(t, extensionCatalog, clusterExtension, sa)
	defer getArtifactsOutput(t)

	clusterExtension.Spec = ocv1alpha1.ClusterExtensionSpec{
		Source: ocv1alpha1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1alpha1.CatalogSource{
				PackageName: "prometheus",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
				},
			},
		},
		Install: ocv1alpha1.ClusterExtensionInstallConfig{
			Namespace: "default",
			ServiceAccount: ocv1alpha1.ServiceAccountReference{
				Name: sa.Name,
			},
		},
	}
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)

		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.1.2.0",
				Version: "1.2.0",
			}},
			clusterExtension.Status.Resolution,
		)
	}, pollDuration, pollInterval)

	// update tag on test-catalog image with v2 image
	t.Log("By updating the catalog tag to point to the v2 catalog")
	v2Image := fmt.Sprintf("%s/%s", os.Getenv("CLUSTER_REGISTRY_HOST"), os.Getenv("E2E_TEST_CATALOG_V2"))
	err = crane.Tag(v2Image, latestImageTag, crane.Insecure)
	require.NoError(t, err)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: extensionCatalog.Name}, extensionCatalog))
		cond := apimeta.FindStatusCondition(extensionCatalog.Status.Conditions, catalogd.TypeServing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, catalogd.ReasonAvailable, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)

		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.2.0.0",
				Version: "2.0.0",
			}},
			clusterExtension.Status.Resolution,
		)
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallReResolvesWhenManagedContentChanged(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("It resolves again when managed content is changed")
	clusterExtension, extensionCatalog, sa := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, sa)
	defer getArtifactsOutput(t)

	clusterExtension.Spec = ocv1alpha1.ClusterExtensionSpec{
		Source: ocv1alpha1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1alpha1.CatalogSource{
				PackageName: "prometheus",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
				},
			},
		},
		Install: ocv1alpha1.ClusterExtensionInstallConfig{
			Namespace: "default",
			ServiceAccount: ocv1alpha1.ServiceAccountReference{
				Name: sa.Name,
			},
		},
	}
	t.Log("It installs the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By reporting a successful installation")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)
		assert.Contains(ct, cond.Message, "Installed bundle")
		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.1.2.0",
				Version: "1.2.0",
			}},
			clusterExtension.Status.Resolution,
		)
	}, pollDuration, pollInterval)

	t.Log("By deleting a managed resource")
	prometheusService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheus-operator",
			Namespace: clusterExtension.Spec.Install.Namespace,
		},
	}
	require.NoError(t, c.Delete(context.Background(), prometheusService))

	t.Log("By eventually re-creating the managed resource")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: prometheusService.Name, Namespace: prometheusService.Namespace}, prometheusService))
	}, pollDuration, pollInterval)
}

func TestClusterExtensionRecoversFromInitialInstallFailedWhenFailureFixed(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When the extension bundle format is registry+v1")

	clusterExtension, extensionCatalog, _ := testInit(t)
	name := rand.String(10)
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
	}
	err := c.Create(context.Background(), sa)
	require.NoError(t, err)

	defer testCleanup(t, extensionCatalog, clusterExtension, sa)
	defer getArtifactsOutput(t)

	clusterExtension.Spec = ocv1alpha1.ClusterExtensionSpec{
		Source: ocv1alpha1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1alpha1.CatalogSource{
				PackageName: "prometheus",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
				},
			},
		},
		Install: ocv1alpha1.ClusterExtensionInstallConfig{
			Namespace: "default",
			ServiceAccount: ocv1alpha1.ServiceAccountReference{
				Name: sa.Name,
			},
		},
	}
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		assert.Equal(ct,
			&ocv1alpha1.ClusterExtensionResolutionStatus{Bundle: ocv1alpha1.BundleMetadata{
				Name:    "prometheus-operator.1.2.0",
				Version: "1.2.0",
			}},
			clusterExtension.Status.Resolution,
		)
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting Progressing == True with Reason Retrying")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonRetrying, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually failing to install the package successfully due to insufficient ServiceAccount permissions")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonFailed, cond.Reason)
		assert.Contains(ct, cond.Message, "forbidden")
	}, pollDuration, pollInterval)

	t.Log("By fixing the ServiceAccount permissions")
	require.NoError(t, createClusterRoleAndBindingForSA(context.Background(), name, sa, clusterExtension.Name))

	// NOTE: In order to ensure predictable results we need to ensure we have a single
	// known failure with a singular fix operation. Additionally, due to the exponential
	// backoff of this eventually check we MUST ensure we do not touch the ClusterExtension
	// after creating and binding the needed permissions to the ServiceAccount.
	t.Log("By eventually installing the package successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)
		assert.Contains(ct, cond.Message, "Installed bundle")
		assert.NotEmpty(ct, clusterExtension.Status.Install)
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting Progressing == False with Reason Success")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)
	}, pollDuration, pollInterval)
}

// getArtifactsOutput gets all the artifacts from the test run and saves them to the artifact path.
// Currently it saves:
// - clusterextensions
// - pods logs
// - deployments
// - catalogsources
func getArtifactsOutput(t *testing.T) {
	basePath := env.GetString("ARTIFACT_PATH", "")
	if basePath == "" {
		return
	}

	kubeClient, err := kubeclient.NewForConfig(cfg)
	require.NoError(t, err)

	// sanitize the artifact name for use as a directory name
	testName := strings.ReplaceAll(strings.ToLower(t.Name()), " ", "-")
	// Get the test description and sanitize it for use as a directory name
	artifactPath := filepath.Join(basePath, artifactName, fmt.Sprint(time.Now().UnixNano()), testName)

	// Create the full artifact path
	err = os.MkdirAll(artifactPath, 0755)
	require.NoError(t, err)

	// Get all namespaces
	namespaces := corev1.NamespaceList{}
	if err := c.List(context.Background(), &namespaces); err != nil {
		fmt.Printf("Failed to list namespaces: %v", err)
	}

	// get all cluster extensions save them to the artifact path.
	clusterExtensions := ocv1alpha1.ClusterExtensionList{}
	if err := c.List(context.Background(), &clusterExtensions, client.InNamespace("")); err != nil {
		fmt.Printf("Failed to list cluster extensions: %v", err)
	}
	for _, clusterExtension := range clusterExtensions.Items {
		// Save cluster extension to artifact path
		clusterExtensionYaml, err := yaml.Marshal(clusterExtension)
		if err != nil {
			fmt.Printf("Failed to marshal cluster extension: %v", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, clusterExtension.Name+"-clusterextension.yaml"), clusterExtensionYaml, 0600); err != nil {
			fmt.Printf("Failed to write cluster extension to file: %v", err)
		}
	}

	// get all catalogsources save them to the artifact path.
	catalogsources := catalogd.ClusterCatalogList{}
	if err := c.List(context.Background(), &catalogsources, client.InNamespace("")); err != nil {
		fmt.Printf("Failed to list catalogsources: %v", err)
	}
	for _, catalogsource := range catalogsources.Items {
		// Save catalogsource to artifact path
		catalogsourceYaml, err := yaml.Marshal(catalogsource)
		if err != nil {
			fmt.Printf("Failed to marshal catalogsource: %v", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, catalogsource.Name+"-catalogsource.yaml"), catalogsourceYaml, 0600); err != nil {
			fmt.Printf("Failed to write catalogsource to file: %v", err)
		}
	}

	for _, namespace := range namespaces.Items {
		// let's ignore kube-* namespaces.
		if strings.Contains(namespace.Name, "kube-") {
			continue
		}

		namespacedArtifactPath := filepath.Join(artifactPath, namespace.Name)
		if err := os.Mkdir(namespacedArtifactPath, 0755); err != nil {
			fmt.Printf("Failed to create namespaced artifact path: %v", err)
			continue
		}

		// get all deployments in the namespace and save them to the artifact path.
		deployments := appsv1.DeploymentList{}
		if err := c.List(context.Background(), &deployments, client.InNamespace(namespace.Name)); err != nil {
			fmt.Printf("Failed to list deployments %v in namespace: %q", err, namespace.Name)
			continue
		}

		for _, deployment := range deployments.Items {
			// Save deployment to artifact path
			deploymentYaml, err := yaml.Marshal(deployment)
			if err != nil {
				fmt.Printf("Failed to marshal deployment: %v", err)
				continue
			}
			if err := os.WriteFile(filepath.Join(namespacedArtifactPath, deployment.Name+"-deployment.yaml"), deploymentYaml, 0600); err != nil {
				fmt.Printf("Failed to write deployment to file: %v", err)
			}
		}

		// Get logs from all pods in all namespaces
		pods := corev1.PodList{}
		if err := c.List(context.Background(), &pods, client.InNamespace(namespace.Name)); err != nil {
			fmt.Printf("Failed to list pods %v in namespace: %q", err, namespace.Name)
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
				continue
			}
			for _, container := range pod.Spec.Containers {
				logs, err := kubeClient.CoreV1().Pods(namespace.Name).GetLogs(pod.Name, &corev1.PodLogOptions{Container: container.Name}).Stream(context.Background())
				if err != nil {
					fmt.Printf("Failed to get logs for pod %q in namespace %q: %v", pod.Name, namespace.Name, err)
					continue
				}
				defer logs.Close()

				outFile, err := os.Create(filepath.Join(namespacedArtifactPath, pod.Name+"-"+container.Name+"-logs.txt"))
				if err != nil {
					fmt.Printf("Failed to create file for pod %q in namespace %q: %v", pod.Name, namespace.Name, err)
					continue
				}
				defer outFile.Close()

				if _, err := io.Copy(outFile, logs); err != nil {
					fmt.Printf("Failed to copy logs for pod %q in namespace %q: %v", pod.Name, namespace.Name, err)
					continue
				}
			}
		}
	}
}
