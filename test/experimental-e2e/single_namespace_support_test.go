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
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/scheme"
	utils "github.com/operator-framework/operator-controller/internal/shared/util/testutils"
	. "github.com/operator-framework/operator-controller/test/helpers"
)

const (
	artifactName = "operator-controller-experimental-e2e"
	pollDuration = time.Minute
	pollInterval = time.Second
)

var (
	cfg *rest.Config
	c   client.Client
)

func TestMain(m *testing.M) {
	cfg = ctrl.GetConfigOrDie()

	var err error
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme.Scheme))
	c, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	utilruntime.Must(err)

	os.Exit(m.Run())
}

func TestNoop(t *testing.T) {
	t.Log("Running experimental-e2e tests")
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)
}

func TestClusterExtensionSingleNamespaceSupport(t *testing.T) {
	t.Log("Test support for cluster extension config")
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	t.Log("By creating install namespace, watch namespace and necessary rbac resources")
	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "single-namespace-operator",
		},
	}
	require.NoError(t, c.Create(t.Context(), &namespace))
	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), &namespace))
	})

	watchNamespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "single-namespace-operator-target",
		},
	}
	require.NoError(t, c.Create(t.Context(), &watchNamespace))
	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), &watchNamespace))
	})

	serviceAccount := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "single-namespace-operator-installer",
			Namespace: namespace.GetName(),
		},
	}
	require.NoError(t, c.Create(t.Context(), &serviceAccount))
	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), &serviceAccount))
	})

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "single-namespace-operator-installer",
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

	t.Log("By creating the test-catalog ClusterCatalog")
	extensionCatalog := &ocv1.ClusterCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-catalog",
		},
		Spec: ocv1.ClusterCatalogSpec{
			Source: ocv1.CatalogSource{
				Type: ocv1.SourceTypeImage,
				Image: &ocv1.ImageSource{
					Ref:                 fmt.Sprintf("%s/e2e/test-catalog:v1", os.Getenv("CLUSTER_REGISTRY_HOST")),
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

	t.Log("By attempting to install the single-namespace-operator ClusterExtension without any configuration")
	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "single-namespace-operator-extension",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: "single-namespace-operator",
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

	t.Log("By waiting for single-namespace-operator extension installation to fail")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(t.Context(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
		require.Contains(ct, cond.Message, "required field \"watchNamespace\" is missing")
	}, pollDuration, pollInterval)

	t.Log("By updating the ClusterExtension configuration with a watchNamespace")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterExtension.GetName()}, clusterExtension))
		clusterExtension.Spec.Config = &ocv1.ClusterExtensionConfig{
			ConfigType: ocv1.ClusterExtensionConfigTypeInline,
			Inline: &apiextensionsv1.JSON{
				Raw: []byte(fmt.Sprintf(`{"watchNamespace": "%s"}`, watchNamespace.GetName())),
			},
		}
		require.NoError(t, c.Update(t.Context(), clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By waiting for single-namespace-operator extension to be installed successfully")
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

	t.Log("By ensuring the single-namespace-operator deployment is correctly configured to watch the watch namespace")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		deployment := &appsv1.Deployment{}
		require.NoError(ct, c.Get(t.Context(), types.NamespacedName{Namespace: namespace.GetName(), Name: "single-namespace-operator"}, deployment))
		require.NotNil(ct, deployment.Spec.Template.GetAnnotations())
		require.Equal(ct, watchNamespace.GetName(), deployment.Spec.Template.GetAnnotations()["olm.targetNamespaces"])
	}, pollDuration, pollInterval)
}

func TestClusterExtensionOwnNamespaceSupport(t *testing.T) {
	t.Log("Test support for cluster extension with OwnNamespace install mode support")
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	t.Log("By creating install namespace, watch namespace and necessary rbac resources")
	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "own-namespace-operator",
		},
	}
	require.NoError(t, c.Create(t.Context(), &namespace))
	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), &namespace))
	})

	serviceAccount := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "own-namespace-operator-installer",
			Namespace: namespace.GetName(),
		},
	}
	require.NoError(t, c.Create(t.Context(), &serviceAccount))
	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), &serviceAccount))
	})

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "own-namespace-operator-installer",
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

	t.Log("By creating the test-catalog ClusterCatalog")
	extensionCatalog := &ocv1.ClusterCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-catalog",
		},
		Spec: ocv1.ClusterCatalogSpec{
			Source: ocv1.CatalogSource{
				Type: ocv1.SourceTypeImage,
				Image: &ocv1.ImageSource{
					Ref:                 fmt.Sprintf("%s/e2e/test-catalog:v1", os.Getenv("CLUSTER_REGISTRY_HOST")),
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

	t.Log("By attempting to install the own-namespace-operator ClusterExtension without any configuration")
	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "own-namespace-operator-extension",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: "own-namespace-operator",
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

	t.Log("By waiting for own-namespace-operator extension installation to fail")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(t.Context(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
		require.Contains(ct, cond.Message, "required field \"watchNamespace\" is missing")
	}, pollDuration, pollInterval)

	t.Log("By updating the ClusterExtension configuration with a watchNamespace other than the install namespace")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterExtension.GetName()}, clusterExtension))
		clusterExtension.Spec.Config = &ocv1.ClusterExtensionConfig{
			ConfigType: ocv1.ClusterExtensionConfigTypeInline,
			Inline: &apiextensionsv1.JSON{
				Raw: []byte(`{"watchNamespace": "some-namespace"}`),
			},
		}
		require.NoError(t, c.Update(t.Context(), clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By waiting for own-namespace-operator extension installation to fail")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(t.Context(), types.NamespacedName{Name: clusterExtension.Name}, clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonRetrying, cond.Reason)
		require.Contains(ct, cond.Message, fmt.Sprintf("invalid 'watchNamespace' \"some-namespace\": must be install namespace (%s)", clusterExtension.Spec.Namespace))
	}, pollDuration, pollInterval)

	t.Log("By updating the ClusterExtension configuration with a watchNamespace = install namespace")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterExtension.GetName()}, clusterExtension))
		clusterExtension.Spec.Config = &ocv1.ClusterExtensionConfig{
			ConfigType: ocv1.ClusterExtensionConfigTypeInline,
			Inline: &apiextensionsv1.JSON{
				Raw: []byte(fmt.Sprintf(`{"watchNamespace": "%s"}`, clusterExtension.Spec.Namespace)),
			},
		}
		require.NoError(t, c.Update(t.Context(), clusterExtension))
	}, pollDuration, pollInterval)

	t.Log("By waiting for own-namespace-operator extension to be installed successfully")
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

	t.Log("By ensuring the own-namespace-operator deployment is correctly configured to watch the watch namespace")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		deployment := &appsv1.Deployment{}
		require.NoError(ct, c.Get(t.Context(), types.NamespacedName{Namespace: namespace.GetName(), Name: "own-namespace-operator"}, deployment))
		require.NotNil(ct, deployment.Spec.Template.GetAnnotations())
		require.Equal(ct, clusterExtension.Spec.Namespace, deployment.Spec.Template.GetAnnotations()["olm.targetNamespaces"])
	}, pollDuration, pollInterval)
}

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
