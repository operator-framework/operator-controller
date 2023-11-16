package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/utils/env"

	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

const (
	artifactName = "operator-controller-e2e"
)

var pollDuration = time.Minute
var pollInterval = time.Second

func testInit(t *testing.T) (*operatorv1alpha1.Operator, string, *catalogd.Catalog) {
	var err error
	operatorCatalog, err := createTestCatalog(context.Background(), testCatalogName, os.Getenv(testCatalogRefEnvVar))
	require.NoError(t, err)

	operatorName := fmt.Sprintf("operator-%s", rand.String(8))
	operator := &operatorv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name: operatorName,
		},
	}
	return operator, operatorName, operatorCatalog
}

func testCleanup(t *testing.T, cat *catalogd.Catalog, operator *operatorv1alpha1.Operator) {
	require.NoError(t, c.Delete(context.Background(), cat))
	require.Eventually(t, func() bool {
		err := c.Get(context.Background(), types.NamespacedName{Name: cat.Name}, &catalogd.Catalog{})
		return errors.IsNotFound(err)
	}, pollDuration, pollInterval)
	require.NoError(t, c.Delete(context.Background(), operator))
	require.Eventually(t, func() bool {
		err := c.Get(context.Background(), types.NamespacedName{Name: operator.Name}, &operatorv1alpha1.Operator{})
		return errors.IsNotFound(err)
	}, pollDuration, pollInterval)
}

func TestOperatorInstallRegistry(t *testing.T) {
	t.Log("When an operator is installed from an operator catalog")
	t.Log("When the operator bundle format is registry+v1")

	operator, operatorName, operatorCatalog := testInit(t)
	defer testCleanup(t, operatorCatalog, operator)
	defer getArtifactsOutput(t)

	operator.Spec = operatorv1alpha1.OperatorSpec{
		PackageName: "prometheus",
	}
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the Operator resource")
	require.NoError(t, c.Create(context.Background(), operator))

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operator.Name}, operator))
		assert.Len(ct, operator.Status.Conditions, 2)
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, operatorv1alpha1.ReasonSuccess, cond.Reason)
		assert.Contains(ct, cond.Message, "resolved to")
		assert.Equal(ct, "localhost/testdata/bundles/registry-v1/prometheus-operator:v2.0.0", operator.Status.ResolvedBundleResource)
	}, pollDuration, pollInterval)

	t.Log("By eventually installing the package successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operator.Name}, operator))
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeInstalled)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, operatorv1alpha1.ReasonSuccess, cond.Reason)
		assert.Contains(ct, cond.Message, "installed from")
		assert.NotEmpty(ct, operator.Status.InstalledBundleResource)

		bd := rukpakv1alpha1.BundleDeployment{}
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operatorName}, &bd))
		hasValidBundle := apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeHasValidBundle)
		if !assert.NotNil(ct, hasValidBundle) {
			return
		}
		assert.Equal(ct, rukpakv1alpha1.ReasonUnpackSuccessful, hasValidBundle.Reason)
		installed := apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeInstalled)
		if !assert.NotNil(ct, installed) {
			return
		}
		assert.Equal(ct, rukpakv1alpha1.ReasonInstallationSucceeded, installed.Reason)
	}, pollDuration, pollInterval)
}

func TestOperatorInstallPlain(t *testing.T) {
	t.Log("When an operator is installed from an operator catalog")
	t.Log("When the operator bundle format is plain+v0")

	operator, operatorName, operatorCatalog := testInit(t)
	defer testCleanup(t, operatorCatalog, operator)
	defer getArtifactsOutput(t)

	operator.Spec = operatorv1alpha1.OperatorSpec{
		PackageName: "plain",
	}
	t.Log("It resolves the specified package with correct bundle path")
	t.Log("By creating the Operator resource")
	require.NoError(t, c.Create(context.Background(), operator))

	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operator.Name}, operator))
		assert.Len(ct, operator.Status.Conditions, 2)
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, operatorv1alpha1.ReasonSuccess, cond.Reason)
		assert.Contains(ct, cond.Message, "resolved to")
		assert.NotEmpty(ct, operator.Status.ResolvedBundleResource)
	}, pollDuration, pollInterval)

	t.Log("By eventually installing the package successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operator.Name}, operator))
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeInstalled)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, operatorv1alpha1.ReasonSuccess, cond.Reason)
		assert.Contains(ct, cond.Message, "installed from")
		assert.NotEmpty(ct, operator.Status.InstalledBundleResource)

		bd := rukpakv1alpha1.BundleDeployment{}
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operatorName}, &bd))
		hasValidBundle := apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeHasValidBundle)
		if !assert.NotNil(ct, hasValidBundle) {
			return
		}
		assert.Equal(ct, rukpakv1alpha1.ReasonUnpackSuccessful, hasValidBundle.Reason)
		installed := apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeInstalled)
		if !assert.NotNil(ct, installed) {
			return
		}
		assert.Equal(ct, rukpakv1alpha1.ReasonInstallationSucceeded, installed.Reason)
	}, pollDuration, pollInterval)
}

func TestOperatorInstallReResolvesWhenNewCatalog(t *testing.T) {
	t.Log("When an operator is installed from an operator catalog")
	t.Log("It resolves again when a new catalog is available")

	operator, _, operatorCatalog := testInit(t)
	defer testCleanup(t, operatorCatalog, operator)
	defer getArtifactsOutput(t)

	pkgName := "prometheus"
	operator.Spec = operatorv1alpha1.OperatorSpec{
		PackageName: pkgName,
	}

	t.Log("By deleting the catalog first")
	require.NoError(t, c.Delete(context.Background(), operatorCatalog))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		err := c.Get(context.Background(), types.NamespacedName{Name: operatorCatalog.Name}, &catalogd.Catalog{})
		assert.True(ct, errors.IsNotFound(err))
	}, pollDuration, pollInterval)

	t.Log("By creating the Operator resource")
	require.NoError(t, c.Create(context.Background(), operator))

	t.Log("By failing to find Operator during resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operator.Name}, operator))
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, operatorv1alpha1.ReasonResolutionFailed, cond.Reason)
		assert.Equal(ct, fmt.Sprintf("no package %q found", pkgName), cond.Message)
	}, pollDuration, pollInterval)

	t.Log("By creating an Operator catalog with the desired package")
	var err error
	operatorCatalog, err = createTestCatalog(context.Background(), testCatalogName, os.Getenv(testCatalogRefEnvVar))
	require.NoError(t, err)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operatorCatalog.Name}, operatorCatalog))
		cond := apimeta.FindStatusCondition(operatorCatalog.Status.Conditions, catalogd.TypeUnpacked)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, catalogd.ReasonUnpackSuccessful, cond.Reason)
	}, pollDuration, pollInterval)

	t.Log("By eventually resolving the package successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operator.Name}, operator))
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, operatorv1alpha1.ReasonSuccess, cond.Reason)
	}, pollDuration, pollInterval)
}

func TestOperatorInstallNonSuccessorVersion(t *testing.T) {
	t.Log("When an operator is installed from an operator catalog")
	t.Log("When resolving upgrade edges")

	operator, _, operatorCatalog := testInit(t)
	defer testCleanup(t, operatorCatalog, operator)
	defer getArtifactsOutput(t)

	t.Log("By creating an Operator at a specified version")
	operator.Spec = operatorv1alpha1.OperatorSpec{
		PackageName: "prometheus",
		Version:     "1.0.0",
	}
	require.NoError(t, c.Create(context.Background(), operator))
	t.Log("By eventually reporting a successful resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operator.Name}, operator))
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, operatorv1alpha1.ReasonSuccess, cond.Reason)
		assert.Contains(ct, cond.Message, "resolved to")
		assert.Equal(ct, "localhost/testdata/bundles/registry-v1/prometheus-operator:v1.0.0", operator.Status.ResolvedBundleResource)
	}, pollDuration, pollInterval)

	t.Log("It does not allow to upgrade the Operator to a non-successor version")
	t.Log("By updating the Operator resource to a non-successor version")
	// Semver only allows upgrades within major version at the moment.
	operator.Spec.Version = "2.0.0"
	require.NoError(t, c.Update(context.Background(), operator))
	t.Log("By eventually reporting an unsatisfiable resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operator.Name}, operator))
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, operatorv1alpha1.ReasonResolutionFailed, cond.Reason)
		assert.Contains(ct, cond.Message, "constraints not satisfiable")
		assert.Contains(ct, cond.Message, "installed package prometheus requires at least one of test-catalog-prometheus-prometheus-operator.1.2.0, test-catalog-prometheus-prometheus-operator.1.0.1, test-catalog-prometheus-prometheus-operator.1.0.0")
		assert.Empty(ct, operator.Status.ResolvedBundleResource)
	}, pollDuration, pollInterval)
}

func TestOperatorInstallSuccessorVersion(t *testing.T) {
	t.Log("When an operator is installed from an operator catalog")
	t.Log("When resolving upgrade edges")
	operator, _, operatorCatalog := testInit(t)
	defer testCleanup(t, operatorCatalog, operator)
	defer getArtifactsOutput(t)

	t.Log("By creating an Operator at a specified version")
	operator.Spec = operatorv1alpha1.OperatorSpec{
		PackageName: "prometheus",
		Version:     "1.0.0",
	}
	require.NoError(t, c.Create(context.Background(), operator))
	t.Log("By eventually reporting a successful resolution")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operator.Name}, operator))
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, operatorv1alpha1.ReasonSuccess, cond.Reason)
		assert.Contains(ct, cond.Message, "resolved to")
		assert.Equal(ct, "localhost/testdata/bundles/registry-v1/prometheus-operator:v1.0.0", operator.Status.ResolvedBundleResource)
	}, pollDuration, pollInterval)

	t.Log("It does allow to upgrade the Operator to any of the successor versions within non-zero major version")
	t.Log("By updating the Operator resource by skipping versions")
	// Test catalog has versions between the initial version and new version
	operator.Spec.Version = "1.2.0"
	require.NoError(t, c.Update(context.Background(), operator))
	t.Log("By eventually reporting a successful resolution and bundle path")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(context.Background(), types.NamespacedName{Name: operator.Name}, operator))
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, operatorv1alpha1.ReasonSuccess, cond.Reason)
		assert.Contains(ct, cond.Message, "resolved to")
		assert.Equal(ct, "localhost/testdata/bundles/registry-v1/prometheus-operator:v1.2.0", operator.Status.ResolvedBundleResource)
	}, pollDuration, pollInterval)
}

// getArtifactsOutput gets all the artifacts from the test run and saves them to the artifact path.
// Currently it saves:
// - operators
// - pods logs
// - deployments
// - bundle
// - bundledeployments
// - catalogsources
func getArtifactsOutput(t *testing.T) {
	basePath := env.GetString("ARTIFACT_PATH", "")
	if basePath == "" {
		return
	}

	kubeClient, err := kubeclient.NewForConfig(cfg)
	require.NoError(t, err)

	// sanitize the artifact name for use as a directory name
	testName := strings.ReplaceAll(strings.ToLower(t.Name()), " ", "-")
	// Get the test description and sanitize it for use as a directory name
	artifactPath := filepath.Join(basePath, artifactName, fmt.Sprint(time.Now().UnixNano()), testName)

	// Create the full artifact path
	err = os.MkdirAll(artifactPath, 0755)
	require.NoError(t, err)

	// Get all namespaces
	namespaces := corev1.NamespaceList{}
	if err := c.List(context.Background(), &namespaces); err != nil {
		fmt.Printf("Failed to list namespaces %v", err)
	}

	// get all operators save them to the artifact path.
	operators := operatorv1alpha1.OperatorList{}
	if err := c.List(context.Background(), &operators, client.InNamespace("")); err != nil {
		fmt.Printf("Failed to list operators %v", err)
	}
	for _, operator := range operators.Items {
		// Save operator to artifact path
		operatorYaml, err := yaml.Marshal(operator)
		if err != nil {
			fmt.Printf("Failed to marshal operator %v", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, operator.Name+"-operator.yaml"), operatorYaml, 0600); err != nil {
			fmt.Printf("Failed to write operator to file %v", err)
		}
	}

	// get all catalogsources save them to the artifact path.
	catalogsources := catalogd.CatalogList{}
	if err := c.List(context.Background(), &catalogsources, client.InNamespace("")); err != nil {
		fmt.Printf("Failed to list catalogsources %v", err)
	}
	for _, catalogsource := range catalogsources.Items {
		// Save catalogsource to artifact path
		catalogsourceYaml, err := yaml.Marshal(catalogsource)
		if err != nil {
			fmt.Printf("Failed to marshal catalogsource %v", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, catalogsource.Name+"-catalogsource.yaml"), catalogsourceYaml, 0600); err != nil {
			fmt.Printf("Failed to write catalogsource to file %v", err)
		}
	}

	// Get all Bundles in the namespace and save them to the artifact path.
	bundles := rukpakv1alpha1.BundleList{}
	if err := c.List(context.Background(), &bundles, client.InNamespace("")); err != nil {
		fmt.Printf("Failed to list bundles %v", err)
	}
	for _, bundle := range bundles.Items {
		// Save bundle to artifact path
		bundleYaml, err := yaml.Marshal(bundle)
		if err != nil {
			fmt.Printf("Failed to marshal bundle %v", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, bundle.Name+"-bundle.yaml"), bundleYaml, 0600); err != nil {
			fmt.Printf("Failed to write bundle to file %v", err)
		}
	}

	// Get all BundleDeployments in the namespace and save them to the artifact path.
	bundleDeployments := rukpakv1alpha1.BundleDeploymentList{}
	if err := c.List(context.Background(), &bundleDeployments, client.InNamespace("")); err != nil {
		fmt.Printf("Failed to list bundleDeployments %v", err)
	}
	for _, bundleDeployment := range bundleDeployments.Items {
		// Save bundleDeployment to artifact path
		bundleDeploymentYaml, err := yaml.Marshal(bundleDeployment)
		if err != nil {
			fmt.Printf("Failed to marshal bundleDeployment %v", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, bundleDeployment.Name+"-bundleDeployment.yaml"), bundleDeploymentYaml, 0600); err != nil {
			fmt.Printf("Failed to write bundleDeployment to file %v", err)
		}
	}

	for _, namespace := range namespaces.Items {
		// let's ignore kube-* namespaces.
		if strings.Contains(namespace.Name, "kube-") {
			continue
		}

		namespacedArtifactPath := filepath.Join(artifactPath, namespace.Name)
		if err := os.Mkdir(namespacedArtifactPath, 0755); err != nil {
			fmt.Printf("Failed to create namespaced artifact path %v", err)
			continue
		}

		// get all deployments in the namespace and save them to the artifact path.
		deployments := appsv1.DeploymentList{}
		if err := c.List(context.Background(), &deployments, client.InNamespace(namespace.Name)); err != nil {
			fmt.Printf("Failed to list deployments %v in namespace: %q", err, namespace.Name)
			continue
		}

		for _, deployment := range deployments.Items {
			// Save deployment to artifact path
			deploymentYaml, err := yaml.Marshal(deployment)
			if err != nil {
				fmt.Printf("Failed to marshal deployment %v", err)
				continue
			}
			if err := os.WriteFile(filepath.Join(namespacedArtifactPath, deployment.Name+"-deployment.yaml"), deploymentYaml, 0600); err != nil {
				fmt.Printf("Failed to write deployment to file %v", err)
			}
		}

		// Get logs from all pods in all namespaces
		pods := corev1.PodList{}
		if err := c.List(context.Background(), &pods, client.InNamespace(namespace.Name)); err != nil {
			fmt.Printf("Failed to list pods %v in namespace: %q", err, namespace.Name)
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
				continue
			}
			for _, container := range pod.Spec.Containers {
				logs, err := kubeClient.CoreV1().Pods(namespace.Name).GetLogs(pod.Name, &corev1.PodLogOptions{Container: container.Name}).Stream(context.Background())
				if err != nil {
					fmt.Printf("Failed to get logs for pod %q in namespace %q: %v", pod.Name, namespace.Name, err)
					continue
				}
				defer logs.Close()

				outFile, err := os.Create(filepath.Join(namespacedArtifactPath, pod.Name+"-"+container.Name+"-logs.txt"))
				if err != nil {
					fmt.Printf("Failed to create file for pod %q in namespace %q: %v", pod.Name, namespace.Name, err)
					continue
				}
				defer outFile.Close()

				if _, err := io.Copy(outFile, logs); err != nil {
					fmt.Printf("Failed to copy logs for pod %q in namespace %q: %v", pod.Name, namespace.Name, err)
					continue
				}
			}
		}
	}
}
