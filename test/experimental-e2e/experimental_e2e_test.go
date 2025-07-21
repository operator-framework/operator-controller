package experimental_e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/scheme"
	"github.com/operator-framework/operator-controller/test/utils"
)

const (
	artifactName = "operator-controller-experimental-e2e"
	pollDuration = time.Minute
	pollInterval = time.Second
)

var (
	cfg           *rest.Config
	c             client.Client
	dynamicClient dynamic.Interface
)

func TestMain(m *testing.M) {
	cfg = ctrl.GetConfigOrDie()

	var err error
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme.Scheme))
	c, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	utilruntime.Must(err)

	dynamicClient, err = dynamic.NewForConfig(cfg)
	utilruntime.Must(err)

	os.Exit(m.Run())
}

func TestNoop(t *testing.T) {
	t.Log("Running experimental-e2e tests")
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)
}

func TestWebhookSupport(t *testing.T) {
	t.Log("Test support for bundles with webhooks")
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	t.Log("By creating install namespace, and necessary rbac resources")
	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "webhook-operator",
		},
	}
	require.NoError(t, c.Create(t.Context(), &namespace))
	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), &namespace))
	})

	serviceAccount := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webhook-operator-installer",
			Namespace: namespace.GetName(),
		},
	}
	require.NoError(t, c.Create(t.Context(), &serviceAccount))
	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), &serviceAccount))
	})

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "webhook-operator-installer",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				APIGroup:  corev1.GroupName,
				Name:      serviceAccount.GetName(),
				Namespace: serviceAccount.GetNamespace(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
	}
	require.NoError(t, c.Create(t.Context(), clusterRoleBinding))
	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), clusterRoleBinding))
	})

	t.Log("By creating the webhook-operator ClusterCatalog")
	extensionCatalog := &ocv1.ClusterCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name: "webhook-operator-catalog",
		},
		Spec: ocv1.ClusterCatalogSpec{
			Source: ocv1.CatalogSource{
				Type: ocv1.SourceTypeImage,
				Image: &ocv1.ImageSource{
					Ref:                 fmt.Sprintf("%s/e2e/test-catalog:v1", os.Getenv("LOCAL_REGISTRY_HOST")),
					PollIntervalMinutes: ptr.To(1),
				},
			},
		},
	}
	require.NoError(t, c.Create(t.Context(), extensionCatalog))
	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), extensionCatalog))
	})

	t.Log("By waiting for the catalog to serve its metadata")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: extensionCatalog.GetName()}, extensionCatalog))
		cond := apimeta.FindStatusCondition(extensionCatalog.Status.Conditions, ocv1.TypeServing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonAvailable, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By installing the webhook-operator ClusterExtension")
	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "webhook-operator-extension",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: "webhook-operator",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": extensionCatalog.Name},
					},
				},
			},
			Namespace: namespace.GetName(),
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: serviceAccount.GetName(),
			},
		},
	}
	require.NoError(t, c.Create(t.Context(), clusterExtension))
	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), clusterExtension))
	})

	t.Log("By waiting for webhook-operator extension to be installed successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(t.Context(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		require.Contains(ct, cond.Message, "Installed bundle")
		require.NotNil(ct, clusterExtension.Status.Install)
		require.NotEmpty(ct, clusterExtension.Status.Install.Bundle)
	}, pollDuration, pollInterval)

	t.Log("By waiting for webhook-operator deployment to be available")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		deployment := &appsv1.Deployment{}
		require.NoError(ct, c.Get(t.Context(), types.NamespacedName{Namespace: namespace.GetName(), Name: "webhook-operator-webhook"}, deployment))
		available := false
		for _, cond := range deployment.Status.Conditions {
			if cond.Type == appsv1.DeploymentAvailable {
				available = cond.Status == corev1.ConditionTrue
			}
		}
		require.True(ct, available)
	}, pollDuration, pollInterval)

	v1Gvr := schema.GroupVersionResource{
		Group:    "webhook.operators.coreos.io",
		Version:  "v1",
		Resource: "webhooktests",
	}
	v1Client := dynamicClient.Resource(v1Gvr).Namespace(namespace.GetName())

	t.Log("By eventually seeing that invalid CR creation is rejected by the validating webhook")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		obj := getWebhookOperatorResource("invalid-test-cr", namespace.GetName(), false)
		_, err := v1Client.Create(t.Context(), obj, metav1.CreateOptions{})
		require.Error(ct, err)
		require.Contains(ct, err.Error(), "Invalid value: false: Spec.Valid must be true")
	}, pollDuration, pollInterval)

	var (
		res *unstructured.Unstructured
		err error
		obj = getWebhookOperatorResource("valid-test-cr", namespace.GetName(), true)
	)

	t.Log("By eventually creating a valid CR")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		res, err = v1Client.Create(t.Context(), obj, metav1.CreateOptions{})
		require.NoError(ct, err)
	}, pollDuration, pollInterval)
	t.Cleanup(func() {
		require.NoError(t, v1Client.Delete(context.Background(), obj.GetName(), metav1.DeleteOptions{}))
	})

	require.Equal(t, map[string]interface{}{
		"valid":  true,
		"mutate": true,
	}, res.Object["spec"])

	t.Log("By checking a valid CR is converted to v2 by the conversion webhook")
	v2Gvr := schema.GroupVersionResource{
		Group:    "webhook.operators.coreos.io",
		Version:  "v2",
		Resource: "webhooktests",
	}
	v2Client := dynamicClient.Resource(v2Gvr).Namespace(namespace.GetName())

	t.Log("By eventually getting the valid CR with a v2 client")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		res, err = v2Client.Get(t.Context(), obj.GetName(), metav1.GetOptions{})
		require.NoError(ct, err)
	}, pollDuration, pollInterval)

	t.Log("and verifying that the CR is correctly converted")
	require.Equal(t, map[string]interface{}{
		"conversion": map[string]interface{}{
			"valid":  true,
			"mutate": true,
		},
	}, res.Object["spec"])
}

func getWebhookOperatorResource(name string, namespace string, valid bool) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "webhook.operators.coreos.io/v1",
			"kind":       "webhooktests",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"valid": valid,
			},
		},
	}
}
