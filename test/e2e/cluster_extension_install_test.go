package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
	"github.com/operator-framework/operator-controller/test/utils"
)

const (
	artifactName = "operator-controller-e2e"
)

var pollDuration = time.Minute
var pollInterval = time.Second

func createNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	err := c.Create(ctx, ns)
	if err != nil {
		return nil, err
	}
	return ns, nil
}

func createClusterExtensionPermissions(ctx context.Context, subject rbacv1.Subject, clusterExtensionName string) (*rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding, error) {
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterExtensionName,
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
					"configmaps",
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
		return nil, nil, err
	}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterExtensionName,
		},
		Subjects: []rbacv1.Subject{subject},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterExtensionName,
		},
	}
	err = c.Create(ctx, crb)
	if err != nil {
		return nil, nil, err
	}

	return cr, crb, nil
}

func testInit(t *testing.T) (*ocv1.ClusterExtension, *ocv1.ClusterCatalog, *rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding, *corev1.Namespace) {
	clusterExtension, extensionCatalog, ns := testInitNoPermissions(t)

	subject := rbacv1.Subject{Kind: "User", Name: authentication.SyntheticUserName(*clusterExtension)}
	clusterRole, clusterRoleBinding, err := createClusterExtensionPermissions(context.Background(), subject, clusterExtension.Name)
	require.NoError(t, err)
	return clusterExtension, extensionCatalog, clusterRole, clusterRoleBinding, ns
}

func testInitNoPermissions(t *testing.T) (*ocv1.ClusterExtension, *ocv1.ClusterCatalog, *corev1.Namespace) {
	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("clusterextension-%s", rand.String(8)),
		},
	}

	ns, err := createNamespace(context.Background(), clusterExtension.Name)
	require.NoError(t, err)

	extensionCatalog, err := createTestCatalog(context.Background(), testCatalogName, os.Getenv(testCatalogRefEnvVar))
	require.NoError(t, err)

	validateCatalogUnpack(t)

	return clusterExtension, extensionCatalog, ns
}

func validateCatalogUnpack(t *testing.T) {
	catalog := &ocv1.ClusterCatalog{}
	t.Log("Ensuring ClusterCatalog has Status.Condition of Progressing with a status == True and reason == Succeeded")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		err := c.Get(context.Background(), types.NamespacedName{Name: testCatalogName}, catalog)
		assert.NoError(ct, err)
		cond := apimeta.FindStatusCondition(catalog.Status.Conditions, ocv1.TypeProgressing)
		assert.NotNil(ct, cond)
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("Checking that catalog has the expected metadata label")
	assert.NotNil(t, catalog.ObjectMeta.Labels)
	assert.Contains(t, catalog.ObjectMeta.Labels, "olm.operatorframework.io/metadata.name")
	assert.Equal(t, testCatalogName, catalog.ObjectMeta.Labels["olm.operatorframework.io/metadata.name"])

	t.Log("Ensuring ClusterCatalog has Status.Condition of Type = Serving with status == True")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		err := c.Get(context.Background(), types.NamespacedName{Name: testCatalogName}, catalog)
		assert.NoError(ct, err)
		cond := apimeta.FindStatusCondition(catalog.Status.Conditions, ocv1.TypeServing)
		assert.NotNil(ct, cond)
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, ocv1.ReasonAvailable, cond.Reason)
	}, pollDuration, pollInterval)
}

func ensureNoExtensionResources(t *testing.T, clusterExtensionName string) {
	ls := labels.Set{"olm.operatorframework.io/owner-name": clusterExtensionName}

	// CRDs may take an extra long time to be deleted, and may run into the following error:
	// Condition=Terminating Status=True Reason=InstanceDeletionFailed Message="could not list instances: storage is (re)initializing"
	t.Logf("By waiting for CustomResourceDefinitions of %q to be deleted", clusterExtensionName)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		list := &apiextensionsv1.CustomResourceDefinitionList{}
		err := c.List(context.Background(), list, client.MatchingLabelsSelector{Selector: ls.AsSelector()})
		assert.NoError(ct, err)
		assert.Empty(ct, list.Items)
	}, 5*pollDuration, pollInterval)

	t.Logf("By waiting for ClusterRoleBindings of %q to be deleted", clusterExtensionName)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		list := &rbacv1.ClusterRoleBindingList{}
		err := c.List(context.Background(), list, client.MatchingLabelsSelector{Selector: ls.AsSelector()})
		assert.NoError(ct, err)
		assert.Empty(ct, list.Items)
	}, 2*pollDuration, pollInterval)

	t.Logf("By waiting for ClusterRoles of %q to be deleted", clusterExtensionName)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		list := &rbacv1.ClusterRoleList{}
		err := c.List(context.Background(), list, client.MatchingLabelsSelector{Selector: ls.AsSelector()})
		assert.NoError(ct, err)
		assert.Empty(ct, list.Items)
	}, 2*pollDuration, pollInterval)
}

func testCleanup(t *testing.T, objs ...client.Object) {
	timeoutCtx, cancel := context.WithTimeout(context.Background(), pollDuration)
	defer cancel()
	eg, egCtx := errgroup.WithContext(timeoutCtx)

	var clusterExtensionNames []string
	for _, obj := range objs {
		eg.Go(func() error {
			gvk, err := c.GroupVersionKindFor(obj)
			if err != nil {
				return err
			}
			if gvk.Kind == "ClusterExtension" {
				clusterExtensionNames = append(clusterExtensionNames, obj.GetName())
			}
			t.Logf("By deleting %s %s", gvk.Kind, obj.GetName())
			if err := c.Delete(egCtx, obj, client.PropagationPolicy(metav1.DeletePropagationBackground), client.GracePeriodSeconds(0)); err != nil && !errors.IsNotFound(err) {
				return err
			}
			return wait.PollUntilContextCancel(egCtx, pollInterval, true, func(ctx context.Context) (bool, error) {
				err := c.Get(egCtx, client.ObjectKeyFromObject(obj), obj)
				if errors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			})
		})
	}
	require.NoError(t, eg.Wait())
	for _, clusterExtensionName := range clusterExtensionNames {
		ensureNoExtensionResources(t, clusterExtensionName)
	}
}

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
			// for this e2e via the ./config/components/registries-conf kustomize component as part of the e2e overlay.
			// The goal here is to prove that "mirrored-registry.operator-controller-e2e.svc.cluster.local:5000" is
			// mapped to the "real" registry hostname ("docker-registry.operator-controller-e2e.svc.cluster.local:5000").
			name:        "package requires mirror registry configuration in /etc/containers/registries.conf",
			packageName: "test-mirrored",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log("When a cluster extension is installed from a catalog")
			t.Log("When the extension bundle format is registry+v1")

			clusterExtension, extensionCatalog, clusterRole, clusterRoleBinding, ns := testInit(t)
			defer testCleanup(t, extensionCatalog, clusterExtension, clusterRole, clusterRoleBinding, ns)
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
			}
			t.Log("It resolves the specified package with correct bundle path")
			t.Log("By creating the ClusterExtension resource")
			require.NoError(t, c.Create(context.Background(), clusterExtension))

			t.Log("By eventually reporting a successful resolution and bundle path")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
			}, pollDuration, pollInterval)

			t.Log("By eventually reporting progressing as True")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
				cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
				if assert.NotNil(ct, cond) {
					assert.Equal(ct, metav1.ConditionTrue, cond.Status)
					assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
				}
			}, pollDuration, pollInterval)

			t.Log("By eventually installing the package successfully")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
				cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
				if assert.NotNil(ct, cond) {
					assert.Equal(ct, metav1.ConditionTrue, cond.Status)
					assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
					assert.Contains(ct, cond.Message, "Installed bundle")
					assert.NotEmpty(ct, clusterExtension.Status.Install.Bundle)
				}
			}, pollDuration, pollInterval)
		})
	}
}

func TestClusterExtensionInstallWithDeprecatedServiceAccount(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog using the deprecated service account feature")
	t.Log("When the extension bundle format is registry+v1")

	clusterExtension, extensionCatalog, ns := testInitNoPermissions(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, ns)
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      clusterExtension.Name,
		},
	}
	require.NoError(t, c.Create(context.Background(), sa))

	subject := rbacv1.Subject{
		Kind:      "ServiceAccount",
		Name:      clusterExtension.Name,
		Namespace: ns.Name,
	}
	cr, crb, err := createClusterExtensionPermissions(context.Background(), subject, clusterExtension.Name)
	require.NoError(t, err)
	defer testCleanup(t, crb, cr)

	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: ocv1.SourceTypeCatalog,
			Catalog: &ocv1.CatalogFilter{
				PackageName: "test",
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
				},
			},
		},
		Namespace:      ns.Name,
		ServiceAccount: &ocv1.ServiceAccountReference{Name: sa.Name},
	}
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting progressing as True")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
	}, pollDuration, pollInterval)

	t.Log("By eventually installing the package successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
			assert.Contains(ct, cond.Message, "Installed bundle")
			assert.NotEmpty(ct, clusterExtension.Status.Install.Bundle)
		}
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallRegistryDynamic(t *testing.T) {
	// NOTE: Like 'TestClusterExtensionInstallRegistry', this test also requires extra configuration in /etc/containers/registries.conf
	packageName := "dynamic"

	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When the extension bundle format is registry+v1")

	clusterExtension, extensionCatalog, clusterRole, clusterRoleBinding, ns := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, clusterRole, clusterRoleBinding, ns)
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
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
	}, 2*time.Minute, pollInterval)

	// Give the check 2 minutes instead of the typical 1 for the pod's
	// files to update from the configmap change.
	// The theoretical max time is the kubelet sync period of 1 minute +
	// ConfigMap cache TTL of 1 minute = 2 minutes
	t.Log("By eventually reporting progressing as True")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
	}, 2*time.Minute, pollInterval)

	t.Log("By eventually installing the package successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
			assert.Contains(ct, cond.Message, "Installed bundle")
			assert.NotEmpty(ct, clusterExtension.Status.Install.Bundle)
		}
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallRegistryMultipleBundles(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")

	clusterExtension, extensionCatalog, clusterRole, clusterRoleBinding, ns := testInit(t)
	extraCatalog, err := createTestCatalog(context.Background(), "extra-test-catalog", os.Getenv(testCatalogRefEnvVar))
	require.NoError(t, err)

	defer testCleanup(t, extensionCatalog, clusterExtension, clusterRole, clusterRoleBinding, ns)
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
	}
	t.Log("It resolves to multiple bundle paths")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually reporting a failed resolution with multiple bundles")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting Progressing == True and Reason Retrying")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
			assert.Contains(ct, cond.Message, "in multiple catalogs with the same priority [extra-test-catalog test-catalog]")
		}
	}, pollDuration, pollInterval)
}

func TestClusterExtensionBlockInstallNonSuccessorVersion(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When resolving upgrade edges")

	clusterExtension, extensionCatalog, clusterRole, clusterRoleBinding, ns := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, clusterRole, clusterRoleBinding, ns)
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
	}
	require.NoError(t, c.Create(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful installation")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		assert.Equal(ct,
			&ocv1.ClusterExtensionInstallStatus{Bundle: ocv1.BundleMetadata{
				Name:    "test-operator.1.0.0",
				Version: "1.0.0",
			}},
			clusterExtension.Status.Install,
		)

		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
	}, pollDuration, pollInterval)

	t.Log("It does not allow to upgrade the ClusterExtension to a non-successor version")
	t.Log("By updating the ClusterExtension resource to a non-successor version")
	// 1.2.0 does not replace/skip/skipRange 1.0.0.
	clusterExtension.Spec.Source.Catalog.Version = "1.2.0"
	require.NoError(t, c.Update(context.Background(), clusterExtension))
	t.Log("By eventually reporting an unsatisfiable resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting Progressing == True and Reason Retrying")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
			assert.Equal(ct, "error upgrading from currently installed version \"1.0.0\": no bundles found for package \"test\" matching version \"1.2.0\"", cond.Message)
		}
	}, pollDuration, pollInterval)
}

func TestClusterExtensionForceInstallNonSuccessorVersion(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When resolving upgrade edges")

	clusterExtension, extensionCatalog, clusterRole, clusterRoleBinding, ns := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, clusterRole, clusterRoleBinding, ns)
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
	}
	require.NoError(t, c.Create(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
	}, pollDuration, pollInterval)

	t.Log("It allows to upgrade the ClusterExtension to a non-successor version")
	t.Log("By updating the ClusterExtension resource to a non-successor version")
	// 1.2.0 does not replace/skip/skipRange 1.0.0.
	clusterExtension.Spec.Source.Catalog.Version = "1.2.0"
	clusterExtension.Spec.Source.Catalog.UpgradeConstraintPolicy = ocv1.UpgradeConstraintPolicySelfCertified
	require.NoError(t, c.Update(context.Background(), clusterExtension))
	t.Log("By eventually reporting a satisfiable resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallSuccessorVersion(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When resolving upgrade edges")
	clusterExtension, extensionCatalog, clusterRole, clusterRoleBinding, ns := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, clusterRole, clusterRoleBinding, ns)
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
	}
	require.NoError(t, c.Create(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
	}, pollDuration, pollInterval)

	t.Log("It does allow to upgrade the ClusterExtension to any of the successor versions within non-zero major version")
	t.Log("By updating the ClusterExtension resource by skipping versions")
	// 1.0.1 replaces 1.0.0 in the test catalog
	clusterExtension.Spec.Source.Catalog.Version = "1.0.1"
	require.NoError(t, c.Update(context.Background(), clusterExtension))
	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallReResolvesWhenCatalogIsPatched(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("It resolves again when a catalog is patched with new ImageRef")
	clusterExtension, extensionCatalog, clusterRole, clusterRoleBinding, ns := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, clusterRole, clusterRoleBinding, ns)
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
	}
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
	}, pollDuration, pollInterval)

	// patch imageRef tag on test-catalog image with v2 image
	t.Log("By patching the catalog ImageRef to point to the v2 catalog")
	updatedCatalogImage := fmt.Sprintf("%s/test-catalog:v2", os.Getenv("LOCAL_REGISTRY_HOST"))
	err := patchTestCatalog(context.Background(), testCatalogName, updatedCatalogImage)
	require.NoError(t, err)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: extensionCatalog.Name}, extensionCatalog))
		cond := apimeta.FindStatusCondition(extensionCatalog.Status.Conditions, ocv1.TypeServing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonAvailable, cond.Reason)
		}
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
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
	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterExtensionName,
		},
	}
	ns, err := createNamespace(context.Background(), clusterExtensionName)
	require.NoError(t, err)
	subject := rbacv1.Subject{Kind: "User", Name: authentication.SyntheticUserName(*clusterExtension)}
	cr, crb, err := createClusterExtensionPermissions(context.Background(), subject, clusterExtensionName)
	require.NoError(t, err)
	defer testCleanup(t, extensionCatalog, clusterExtension, cr, crb, ns)
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
	}
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
	}, pollDuration, pollInterval)

	// update tag on test-catalog image with v2 image
	t.Log("By updating the catalog tag to point to the v2 catalog")
	v2Image := fmt.Sprintf("%s/%s", os.Getenv("CLUSTER_REGISTRY_HOST"), os.Getenv("E2E_TEST_CATALOG_V2"))
	err = crane.Tag(v2Image, latestImageTag, crane.Insecure)
	require.NoError(t, err)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: extensionCatalog.Name}, extensionCatalog))
		cond := apimeta.FindStatusCondition(extensionCatalog.Status.Conditions, ocv1.TypeServing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonAvailable, cond.Reason)
		}
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstallReResolvesWhenManagedContentChanged(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("It resolves again when managed content is changed")
	clusterExtension, extensionCatalog, clusterRole, clusterRoleBinding, ns := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, clusterRole, clusterRoleBinding, ns)
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
	}
	t.Log("It installs the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By reporting a successful installation")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
			assert.Contains(ct, cond.Message, "Installed bundle")
		}
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
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: testConfigMap.Name, Namespace: testConfigMap.Namespace}, testConfigMap))
	}, pollDuration, pollInterval)
}

func TestClusterExtensionRecoversFromInitialInstallFailedWhenFailureFixed(t *testing.T) {
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When the extension bundle format is registry+v1")

	clusterExtension, extensionCatalog, ns := testInitNoPermissions(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, ns)
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
	}
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting Progressing == True with Reason Retrying")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
		}
	}, pollDuration, pollInterval)

	t.Log("By eventually failing to install the package successfully due to insufficient permissions")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionFalse, cond.Status)
			assert.Equal(ct, ocv1.ReasonFailed, cond.Reason)
			assert.Equal(ct, "No bundle installed", cond.Message)
		}
	}, pollDuration, pollInterval)

	t.Log("By fixing the ClusterExtension admin permissions")
	subject := rbacv1.Subject{Kind: rbacv1.UserKind, Name: authentication.SyntheticUserName(*clusterExtension)}
	cr, crb, err := createClusterExtensionPermissions(context.Background(), subject, clusterExtension.Name)
	require.NoError(t, err)
	defer testCleanup(t, crb, cr)

	// NOTE: In order to ensure predictable results we need to ensure we have a single
	// known failure with a singular fix operation. Additionally, due to the exponential
	// backoff of this eventually check we MUST ensure we do not touch the ClusterExtension
	// after creating and binding the needed permissions.
	t.Log("By eventually installing the package successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
			assert.Contains(ct, cond.Message, "Installed bundle")
			assert.NotEmpty(ct, clusterExtension.Status.Install)
		}
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting Progressing == True with Reason Success")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		}
	}, pollDuration, pollInterval)
}
