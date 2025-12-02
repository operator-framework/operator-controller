package utils

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/scheme"
)

var (
	cfg *rest.Config
	c   client.Client
)

const (
	pollDuration         = time.Minute
	pollInterval         = time.Second
	testCatalogName      = "test-catalog"
	testCatalogRefEnvVar = "CATALOG_IMG"
)

func CreateNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
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

func CreateServiceAccount(ctx context.Context, name types.NamespacedName, clusterExtensionName string) (*corev1.ServiceAccount, error) {
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

	return sa, CreateClusterRoleAndBindingForSA(ctx, name.Name, sa, clusterExtensionName)
}

func CreateClusterRoleAndBindingForSA(ctx context.Context, name string, sa *corev1.ServiceAccount, clusterExtensionName string) error {
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
			{
				APIGroups: []string{
					"networking.k8s.io",
				},
				Resources: []string{
					"networkpolicies",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
					"create",
					"update",
					"patch",
					"delete",
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

func TestInit(t *testing.T) (*ocv1.ClusterExtension, *ocv1.ClusterCatalog, *corev1.ServiceAccount, *corev1.Namespace) {
	ce, cc := TestInitClusterExtensionClusterCatalog(t)
	sa, ns := TestInitServiceAccountNamespace(t, ce.Name)
	return ce, cc, sa, ns
}

func TestInitClusterExtensionClusterCatalog(t *testing.T) (*ocv1.ClusterExtension, *ocv1.ClusterCatalog) {
	ceName := fmt.Sprintf("clusterextension-%s", rand.String(8))
	catalogName := fmt.Sprintf("test-catalog-%s", rand.String(8))

	ce := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: ceName,
		},
	}

	cc, err := CreateTestCatalog(context.Background(), catalogName, os.Getenv(testCatalogRefEnvVar))
	require.NoError(t, err)

	ValidateCatalogUnpackWithName(t, catalogName)

	return ce, cc
}

func TestInitServiceAccountNamespace(t *testing.T, clusterExtensionName string) (*corev1.ServiceAccount, *corev1.Namespace) {
	var err error

	ns, err := CreateNamespace(context.Background(), clusterExtensionName)
	require.NoError(t, err)

	name := types.NamespacedName{
		Name:      clusterExtensionName,
		Namespace: ns.GetName(),
	}

	sa, err := CreateServiceAccount(context.Background(), name, clusterExtensionName)
	require.NoError(t, err)

	return sa, ns
}

// ValidateCatalogUnpack validates that the test catalog with the default name has unpacked successfully.
// Deprecated: Use ValidateCatalogUnpackWithName for tests that use unique catalog names.
func ValidateCatalogUnpack(t *testing.T) {
	ValidateCatalogUnpackWithName(t, testCatalogName)
}

// ValidateCatalogUnpackWithName validates that a catalog with the given name has unpacked successfully.
func ValidateCatalogUnpackWithName(t *testing.T, catalogName string) {
	catalog := &ocv1.ClusterCatalog{}
	t.Log("Ensuring ClusterCatalog has Status.Condition of Progressing with a status == True and reason == Succeeded")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		err := c.Get(context.Background(), types.NamespacedName{Name: catalogName}, catalog)
		require.NoError(ct, err)
		cond := apimeta.FindStatusCondition(catalog.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("Checking that catalog has the expected metadata label")
	require.NotNil(t, catalog.Labels)
	require.Contains(t, catalog.Labels, "olm.operatorframework.io/metadata.name")
	require.Equal(t, catalogName, catalog.Labels["olm.operatorframework.io/metadata.name"])

	t.Log("Ensuring ClusterCatalog has Status.Condition of Type = Serving with status == True")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		err := c.Get(context.Background(), types.NamespacedName{Name: catalogName}, catalog)
		require.NoError(ct, err)
		cond := apimeta.FindStatusCondition(catalog.Status.Conditions, ocv1.TypeServing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonAvailable, cond.Reason)
	}, pollDuration, pollInterval)
}

func EnsureNoExtensionResources(t *testing.T, clusterExtensionName string) {
	ls := labels.Set{"olm.operatorframework.io/owner-name": clusterExtensionName}

	// CRDs may take an extra long time to be deleted, and may run into the following error:
	// Condition=Terminating Status=True Reason=InstanceDeletionFailed Message="could not list instances: storage is (re)initializing"
	t.Logf("By waiting for CustomResourceDefinitions of %q to be deleted", clusterExtensionName)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		list := &apiextensionsv1.CustomResourceDefinitionList{}
		err := c.List(context.Background(), list, client.MatchingLabelsSelector{Selector: ls.AsSelector()})
		require.NoError(ct, err)
		require.Empty(ct, list.Items)
	}, 5*pollDuration, pollInterval)

	t.Logf("By waiting for ClusterRoleBindings of %q to be deleted", clusterExtensionName)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		list := &rbacv1.ClusterRoleBindingList{}
		err := c.List(context.Background(), list, client.MatchingLabelsSelector{Selector: ls.AsSelector()})
		require.NoError(ct, err)
		require.Empty(ct, list.Items)
	}, 2*pollDuration, pollInterval)

	t.Logf("By waiting for ClusterRoles of %q to be deleted", clusterExtensionName)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		list := &rbacv1.ClusterRoleList{}
		err := c.List(context.Background(), list, client.MatchingLabelsSelector{Selector: ls.AsSelector()})
		require.NoError(ct, err)
		require.Empty(ct, list.Items)
	}, 2*pollDuration, pollInterval)
}

func TestCleanup(t *testing.T, cat *ocv1.ClusterCatalog, clusterExtension *ocv1.ClusterExtension, sa *corev1.ServiceAccount, ns *corev1.Namespace) {
	if cat != nil {
		t.Logf("By deleting ClusterCatalog %q", cat.Name)
		require.NoError(t, c.Delete(context.Background(), cat))
		require.Eventually(t, func() bool {
			err := c.Get(context.Background(), types.NamespacedName{Name: cat.Name}, &ocv1.ClusterCatalog{})
			return errors.IsNotFound(err)
		}, pollDuration, pollInterval)
	}

	if clusterExtension != nil {
		t.Logf("By deleting ClusterExtension %q", clusterExtension.Name)
		require.NoError(t, c.Delete(context.Background(), clusterExtension))
		require.Eventually(t, func() bool {
			err := c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, &ocv1.ClusterExtension{})
			return errors.IsNotFound(err)
		}, pollDuration, pollInterval)
		EnsureNoExtensionResources(t, clusterExtension.Name)
	}

	if sa != nil {
		t.Logf("By deleting ServiceAccount %q", sa.Name)
		require.NoError(t, c.Delete(context.Background(), sa))
		require.Eventually(t, func() bool {
			err := c.Get(context.Background(), types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace}, &corev1.ServiceAccount{})
			return errors.IsNotFound(err)
		}, pollDuration, pollInterval)
	}

	if ns != nil {
		t.Logf("By deleting Namespace %q", ns.Name)
		require.NoError(t, c.Delete(context.Background(), ns))
		require.Eventually(t, func() bool {
			err := c.Get(context.Background(), types.NamespacedName{Name: ns.Name}, &corev1.Namespace{})
			return errors.IsNotFound(err)
		}, pollDuration, pollInterval)
	}
}

// CreateTestCatalog will create a new catalog on the test cluster, provided
// the context, catalog name, and the image reference. It returns the created catalog
// or an error if any errors occurred while creating the catalog.
// Note that catalogd will automatically create the label:
//
//	"olm.operatorframework.io/metadata.name": name
func CreateTestCatalog(ctx context.Context, name string, imageRef string) (*ocv1.ClusterCatalog, error) {
	catalog := &ocv1.ClusterCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ocv1.ClusterCatalogSpec{
			Source: ocv1.CatalogSource{
				Type: ocv1.SourceTypeImage,
				Image: &ocv1.ImageSource{
					Ref:                 imageRef,
					PollIntervalMinutes: ptr.To(1),
				},
			},
		},
	}

	err := c.Create(ctx, catalog)
	return catalog, err
}

func init() {
	cfg = ctrl.GetConfigOrDie()

	var err error
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme.Scheme))
	c, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	utilruntime.Must(err)
}
