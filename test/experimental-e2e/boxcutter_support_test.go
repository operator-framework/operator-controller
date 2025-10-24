package experimental_e2e

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
	. "github.com/operator-framework/operator-controller/test/helpers"
)

func TestClusterExtensionVersionUpdate(t *testing.T) {
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
	t.Log("By forcing update of ClusterExtension resource to a non-successor version")
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
	t.Log("We should have two ClusterExtensionRevision resources")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		cerList := &ocv1.ClusterExtensionRevisionList{}
		require.NoError(ct, c.List(context.Background(), cerList))
		require.Len(ct, cerList.Items, 2)
	}, pollDuration, pollInterval)
}
