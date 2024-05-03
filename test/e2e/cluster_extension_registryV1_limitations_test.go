package e2e

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func TestClusterExtensionPackagesWithWebhooksAreNotAllowed(t *testing.T) {
	ctx := context.Background()
	catalog, err := createTestCatalog(ctx, testCatalogName, os.Getenv(testCatalogRefEnvVar))
	defer func(cat *catalogd.Catalog) {
		require.NoError(t, c.Delete(context.Background(), cat))
		require.Eventually(t, func() bool {
			err := c.Get(context.Background(), types.NamespacedName{Name: cat.Name}, &catalogd.Catalog{})
			return errors.IsNotFound(err)
		}, pollDuration, pollInterval)
	}(catalog)
	require.NoError(t, err)

	deleteClusterExtension := func(clusterExtension *ocv1alpha1.ClusterExtension) {
		require.NoError(t, c.Delete(ctx, clusterExtension))
		require.Eventually(t, func() bool {
			err := c.Get(ctx, types.NamespacedName{Name: clusterExtension.Name}, &ocv1alpha1.ClusterExtension{})
			return errors.IsNotFound(err)
		}, pollDuration, pollInterval)
	}

	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "package-with-webhooks-",
		},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      "package-with-webhooks",
			Version:          "1.0.0",
			InstallNamespace: "default",
		},
	}
	err = c.Create(ctx, clusterExtension)
	defer deleteClusterExtension(clusterExtension)
	require.NoError(t, err)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(ctx, types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))

		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
		assert.NotNil(ct, cond)
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonInstallationFailed, cond.Reason)
		assert.Contains(ct, cond.Message, "webhookDefinitions are not supported")
		assert.Equal(ct, &ocv1alpha1.BundleMetadata{Name: "package-with-webhooks.1.0.0", Version: "1.0.0"}, clusterExtension.Status.ResolvedBundle)
	}, pollDuration, pollInterval)
}
