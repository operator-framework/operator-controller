package e2e

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	. "github.com/operator-framework/operator-controller/internal/shared/util/test"
	. "github.com/operator-framework/operator-controller/test/helpers"
)

func TestClusterExtensionRevision(t *testing.T) {
	SkipIfFeatureGateDisabled(t, string(features.BoxcutterRuntime))
	t.Log("When a cluster extension is installed from a catalog")
	t.Log("When the extension bundle format is registry+v1")

	clusterExtension, extensionCatalog, sa, ns := TestInit(t)
	defer TestCleanup(t, extensionCatalog, clusterExtension, sa, ns)
	defer CollectTestArtifacts(t, artifactName, c, cfg)

	clusterExtension.Spec = ocv1.ClusterExtensionSpec{
		Source: ocv1.SourceConfig{
			SourceType: "Catalog",
			Catalog: &ocv1.CatalogFilter{
				PackageName: "test",
				Version:     "1.0.1",
				// we would also like to force upgrade to 1.0.2, which is not within the upgrade path
				UpgradeConstraintPolicy: ocv1.UpgradeConstraintPolicySelfCertified,
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
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the ClusterExtension resource")
	require.NoError(t, c.Create(context.Background(), clusterExtension))

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By revision-1 eventually reporting Progressing:True:Succeeded and Available:True:ProbesSucceeded conditions")
	var clusterExtensionRevision ocv1.ClusterExtensionRevision
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: fmt.Sprintf("%s-1", clusterExtension.Name)}, &clusterExtensionRevision))
		cond := apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)

		cond = apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ClusterExtensionRevisionReasonProbesSucceeded, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting progressing as True")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually installing the package successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		require.Contains(ct, cond.Message, "Installed bundle")
		require.NotEmpty(ct, clusterExtension.Status.Install.Bundle)
		require.Len(ct, clusterExtension.Status.ActiveRevisions, 1)
		require.Equal(ct, clusterExtension.Status.ActiveRevisions[0].Name, clusterExtensionRevision.Name)
		require.Empty(ct, clusterExtension.Status.ActiveRevisions[0].Conditions)
	}, pollDuration, pollInterval)

	t.Log("Check Deployment Availability Probe")
	t.Log("By making the operator pod not ready")
	podName := getPodName(t, clusterExtension.Spec.Namespace, client.MatchingLabels{"app": "olme2etest"})
	podExec(t, clusterExtension.Spec.Namespace, podName, []string{"rm", "/var/www/ready"})

	t.Log("By revision-1 eventually reporting Progressing:True:Succeeded and Available:False:ProbeFailure conditions")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: fmt.Sprintf("%s-1", clusterExtension.Name)}, &clusterExtensionRevision))
		cond := apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)

		cond = apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionFalse, cond.Status)
		require.Equal(ct, ocv1.ClusterExtensionRevisionReasonProbeFailure, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By propagating Available:False to ClusterExtension")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionFalse, cond.Status)
	}, pollDuration, pollInterval)

	t.Log("By making the operator pod ready")
	podName = getPodName(t, clusterExtension.Spec.Namespace, client.MatchingLabels{"app": "olme2etest"})
	podExec(t, clusterExtension.Spec.Namespace, podName, []string{"touch", "/var/www/ready"})

	t.Log("By revision-1 eventually reporting Progressing:True:Succeeded and Available:True:ProbesSucceeded conditions")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: fmt.Sprintf("%s-1", clusterExtension.Name)}, &clusterExtensionRevision))
		cond := apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)

		cond = apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ClusterExtensionRevisionReasonProbesSucceeded, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By propagating Available:True to ClusterExtension")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
	}, pollDuration, pollInterval)

	t.Log("Check archiving")
	t.Log("By upgrading the cluster extension to v1.2.0")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		clusterExtension.Spec.Source.Catalog.Version = "1.2.0"
		require.NoError(t, c.Update(context.Background(), clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By revision-2 eventually reporting Progressing:True:Succeeded and Available:True:ProbesSucceeded conditions")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: fmt.Sprintf("%s-2", clusterExtension.Name)}, &clusterExtensionRevision))
		cond := apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)

		cond = apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ClusterExtensionRevisionReasonProbesSucceeded, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting progressing, available, and installed as True")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)

		cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		require.Contains(ct, cond.Message, "Installed bundle")
		require.NotEmpty(ct, clusterExtension.Status.Install.Bundle)
	}, pollDuration, pollInterval)

	t.Log("By revision-1 eventually reporting Progressing:False:Archived and Available:Unknown:Archived conditions")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: fmt.Sprintf("%s-1", clusterExtension.Name)}, &clusterExtensionRevision))
		cond := apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionFalse, cond.Status)
		require.Equal(ct, ocv1.ClusterExtensionRevisionReasonArchived, cond.Reason)

		cond = apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionUnknown, cond.Status)
		require.Equal(ct, ocv1.ClusterExtensionRevisionReasonArchived, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By upgrading the cluster extension to v1.0.2 containing bad image reference")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		clusterExtension.Spec.Source.Catalog.Version = "1.0.2"
		require.NoError(t, c.Update(context.Background(), clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By revision-3 eventually reporting Progressing:True:Succeeded and Available:False:ProbeFailure conditions")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: fmt.Sprintf("%s-3", clusterExtension.Name)}, &clusterExtensionRevision))
		cond := apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)

		cond = apimeta.FindStatusCondition(clusterExtensionRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionFalse, cond.Status)
		require.Equal(ct, ocv1.ClusterExtensionRevisionReasonProbeFailure, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually reporting more than one active revision")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		require.Len(ct, clusterExtension.Status.ActiveRevisions, 2)
		require.Equal(ct, clusterExtension.Status.ActiveRevisions[0].Name, fmt.Sprintf("%s-2", clusterExtension.Name))
		require.Equal(ct, clusterExtension.Status.ActiveRevisions[1].Name, fmt.Sprintf("%s-3", clusterExtension.Name))
		require.Empty(ct, clusterExtension.Status.ActiveRevisions[0].Conditions)
		require.NotEmpty(ct, clusterExtension.Status.ActiveRevisions[1].Conditions)
	}, pollDuration, pollInterval)
}

func getPodName(t *testing.T, podNamespace string, matchingLabels client.MatchingLabels) string {
	var podList corev1.PodList
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.List(context.Background(), &podList, client.InNamespace(podNamespace), matchingLabels))
		podList.Items = slices.DeleteFunc(podList.Items, func(pod corev1.Pod) bool {
			// Ignore terminating pods
			return pod.DeletionTimestamp != nil
		})
		require.Len(ct, podList.Items, 1)
	}, pollDuration, pollInterval)
	return podList.Items[0].Name
}

func podExec(t *testing.T, podNamespace string, podName string, cmd []string) {
	req := cs.CoreV1().RESTClient().Post().Resource("pods").Name(podName).Namespace(podNamespace).SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Command: cmd,
		Stdout:  true,
	}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(ctrl.GetConfigOrDie(), "POST", req.URL())
	require.NoError(t, err)
	err = exec.StreamWithContext(context.Background(), remotecommand.StreamOptions{Stdout: os.Stdout})
	require.NoError(t, err)
}
