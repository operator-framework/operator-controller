package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func TestClusterExtensionPackageUniqueness(t *testing.T) {
	ctx := context.Background()
	fieldOwner := client.FieldOwner("operator-controller-e2e")

	deleteClusterExtension := func(clusterExtension *ocv1alpha1.ClusterExtension) {
		require.NoError(t, c.Delete(ctx, clusterExtension))
		require.Eventually(t, func() bool {
			err := c.Get(ctx, types.NamespacedName{Name: clusterExtension.Name}, &ocv1alpha1.ClusterExtension{})
			return errors.IsNotFound(err)
		}, pollDuration, pollInterval)
	}

	const firstResourceName = "test-extension-first"
	const firstResourcePackageName = "package1"

	t.Log("create first resource")
	clusterExtension1 := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: firstResourceName,
		},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      firstResourcePackageName,
			InstallNamespace: "default",
		},
	}
	require.NoError(t, c.Create(ctx, clusterExtension1))
	defer deleteClusterExtension(clusterExtension1)

	t.Log("create second resource with the same package as the first resource")
	clusterExtension2 := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-extension-",
		},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      firstResourcePackageName,
			InstallNamespace: "default",
		},
	}
	err := c.Create(ctx, clusterExtension2)
	require.ErrorContains(t, err, fmt.Sprintf("Package %q is already installed via ClusterExtension %q", firstResourcePackageName, firstResourceName))

	t.Log("create second resource with different package")
	clusterExtension2 = &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-extension-",
		},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      "package2",
			InstallNamespace: "default",
		},
	}
	require.NoError(t, c.Create(ctx, clusterExtension2))
	defer deleteClusterExtension(clusterExtension2)

	t.Log("update second resource with package which already exists on the cluster")
	intent := &ocv1alpha1.ClusterExtension{
		TypeMeta: metav1.TypeMeta{
			APIVersion: ocv1alpha1.GroupVersion.String(),
			Kind:       "ClusterExtension",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterExtension2.Name,
		},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      firstResourcePackageName,
			InstallNamespace: "default",
		},
	}
	err = c.Patch(ctx, intent, client.Apply, client.ForceOwnership, fieldOwner)
	require.ErrorContains(t, err, fmt.Sprintf("Package %q is already installed via ClusterExtension %q", firstResourcePackageName, firstResourceName))

	t.Log("update second resource with package which does not exist on the cluster")
	intent = &ocv1alpha1.ClusterExtension{
		TypeMeta: metav1.TypeMeta{
			APIVersion: ocv1alpha1.GroupVersion.String(),
			Kind:       "ClusterExtension",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterExtension2.Name,
		},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      "package3",
			InstallNamespace: "default",
		},
	}
	require.NoError(t, c.Patch(ctx, intent, client.Apply, client.ForceOwnership, fieldOwner))
}
