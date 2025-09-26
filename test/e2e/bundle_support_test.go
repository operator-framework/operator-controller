package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	utils "github.com/operator-framework/operator-controller/internal/shared/util/testutils"
)

func TestClusterExtensionInstall_SingleNamespaceInstall_Fails(t *testing.T) {
	t.Log("Check bundles with only SingleNamespace install mode support don't install")
	clusterExtension, extensionCatalog, sa, ns := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "single-operator",
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

	t.Log("By creating the ClusterExtension resource for the single-operator")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually getting an error in the Progressing condition")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
		require.Contains(ct, cond.Message, "bundle does not support AllNamespaces install mode")
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstall_OwnNamespaceInstall_Fails(t *testing.T) {
	t.Log("Check bundles with only OwnNamespace install mode support don't install")
	clusterExtension, extensionCatalog, sa, ns := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "own-operator",
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

	t.Log("By creating the ClusterExtension resource for the own-operator")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually getting an error in the Progressing condition")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
		require.Contains(ct, cond.Message, "bundle does not support AllNamespaces install mode")
	}, pollDuration, pollInterval)
}

func TestClusterExtensionInstall_BundlesWithWebhooks_Fail(t *testing.T) {
	t.Log("Check bundles with webhook definitions don't install")
	clusterExtension, extensionCatalog, sa, ns := testInit(t)
	defer testCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "webhook-operator",
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

	t.Log("By creating the ClusterExtension resource for the webhook-operator")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually getting an error in the Progressing condition")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
		require.Contains(ct, cond.Message, "webhookDefinitions are not supported")
	}, pollDuration, pollInterval)
}
