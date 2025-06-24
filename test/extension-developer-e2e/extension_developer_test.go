package extensione2e

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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestExtensionDeveloper(t *testing.T) {
	t.Parallel()
	cfg := ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()

	require.NoError(t, ocv1.AddToScheme(scheme))
	require.NoError(t, ocv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	ctx := context.Background()

	catalog := &ocv1.ClusterCatalog{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "catalog",
		},
		Spec: ocv1.ClusterCatalogSpec{
			Source: ocv1.CatalogSource{
				Type: ocv1.SourceTypeImage,
				Image: &ocv1.ImageSource{
					Ref: os.Getenv("CATALOG_IMG"),
				},
			},
		},
	}
	require.NoError(t, c.Create(context.Background(), catalog))

	installNamespace := "default"

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("serviceaccount-%s", rand.String(8)),
			Namespace: installNamespace,
		},
	}
	require.NoError(t, c.Create(ctx, sa))

	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "registryv1",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: os.Getenv("REG_PKG_NAME"),
				},
			},
			Namespace: installNamespace,
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: sa.Name,
			},
		},
	}

	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("clusterrole-%s", rand.String(8)),
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
				ResourceNames: []string{clusterExtension.Name},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"configmaps",
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
	require.NoError(t, c.Create(ctx, cr))

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("clusterrolebinding-%s", rand.String(8)),
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
			Name:     cr.Name,
		},
	}
	require.NoError(t, c.Create(ctx, crb))

	t.Logf("When creating an ClusterExtension that references a package with a %q bundle type", clusterExtension.Name)
	require.NoError(t, c.Create(context.Background(), clusterExtension))
	t.Log("It should have a status condition type of Installed with a status of True and a reason of Success")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		ext := &ocv1.ClusterExtension{}
		assert.NoError(ct, c.Get(context.Background(), client.ObjectKeyFromObject(clusterExtension), ext))
		cond := meta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, 2*time.Minute, time.Second)
	require.NoError(t, c.Delete(context.Background(), catalog))
	require.NoError(t, c.Delete(context.Background(), clusterExtension))
}
