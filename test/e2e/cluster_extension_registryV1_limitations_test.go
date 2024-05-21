package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func TestClusterExtensionPackagesWithWebhooksAreNotAllowed(t *testing.T) {
	ctx := context.Background()
	clusterExtension, catalog := testInit(t)
	defer testCleanup(t, catalog, clusterExtension)
	defer getArtifactsOutput(t)

	clusterExtension.Spec = ocv1alpha1.ClusterExtensionSpec{
		PackageName:      "package-with-webhooks",
		Version:          "1.0.0",
		InstallNamespace: "default",
	}
	require.NoError(t, c.Create(ctx, clusterExtension))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(ctx, types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, ocv1alpha1.ReasonInstallationFailed, cond.Reason)
		assert.Contains(ct, cond.Message, "webhookDefinitions are not supported")
		assert.Equal(ct, &ocv1alpha1.BundleMetadata{Name: "package-with-webhooks.1.0.0", Version: "1.0.0"}, clusterExtension.Status.ResolvedBundle)
	}, pollDuration, pollInterval)
}
