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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func TestExtensionDeveloper(t *testing.T) {
	t.Parallel()
	cfg := ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()

	require.NoError(t, catalogd.AddToScheme(scheme))
	require.NoError(t, ocv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	ctx := context.Background()
	saName := fmt.Sprintf("serviceaccounts-%s", rand.String(8))
	name := types.NamespacedName{
		Name:      saName,
		Namespace: "default",
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
	}
	require.NoError(t, c.Create(ctx, sa))

	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name.Name,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"*",
				},
				Resources: []string{
					"*",
				},
				Verbs: []string{
					"*",
				},
			},
		},
	}
	require.NoError(t, c.Create(ctx, cr))

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      name.Name,
				Namespace: name.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     name.Name,
		},
	}
	require.NoError(t, c.Create(ctx, crb))

	var clusterExtensions = []*ocv1alpha1.ClusterExtension{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "registryv1",
			},
			Spec: ocv1alpha1.ClusterExtensionSpec{
				PackageName:      os.Getenv("REG_PKG_NAME"),
				InstallNamespace: "default",
				ServiceAccount: ocv1alpha1.ServiceAccountReference{
					Name: saName,
				},
			},
		},
	}

	for _, ce := range clusterExtensions {
		clusterExtension := ce
		t.Run(clusterExtension.ObjectMeta.Name, func(t *testing.T) {
			t.Parallel()
			catalog := &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "catalog",
				},
				Spec: catalogd.ClusterCatalogSpec{
					Source: catalogd.CatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ImageSource{
							Ref:                   os.Getenv("CATALOG_IMG"),
							InsecureSkipTLSVerify: true,
						},
					},
				},
			}
			t.Logf("When creating an ClusterExtension that references a package with a %q bundle type", clusterExtension.ObjectMeta.Name)
			require.NoError(t, c.Create(context.Background(), catalog))
			require.NoError(t, c.Create(context.Background(), clusterExtension))
			t.Log("It should have a status condition type of Installed with a status of True and a reason of Success")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				ext := &ocv1alpha1.ClusterExtension{}
				assert.NoError(ct, c.Get(context.Background(), client.ObjectKeyFromObject(clusterExtension), ext))
				cond := meta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeInstalled)
				if !assert.NotNil(ct, cond) {
					return
				}
				assert.Equal(ct, metav1.ConditionTrue, cond.Status)
				assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)
			}, 2*time.Minute, time.Second)
			require.NoError(t, c.Delete(context.Background(), catalog))
			require.NoError(t, c.Delete(context.Background(), clusterExtension))
		})
	}
}
