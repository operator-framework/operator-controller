package samples

import (
	"github.com/operator-framework/operator-controller/hack/generate-testdata/internal/utils"
	"log"
	"path/filepath"
	pluginutil "sigs.k8s.io/kubebuilder/v4/pkg/plugin/util"
)

// BuildSampleV1 generates test operator v1.0.0
// ---------------------------------------------
// Create a basic Operator using Busybox image.
// This Operator provides an API to deploy and manage a simple workload.
// It is built using kubebuilder with the deploy-image plugin.
// Useful for testing OLM with a minimal Operator.
//
// Commands:
// kubebuilder init
// kubebuilder create api --group example.com --version v1alpha1 --kind Busybox --image=busybox:1.36.1 --plugins="deploy-image/v1-alpha"
func BuildSampleV1(samplesPath string) {
	generateBasicOperator(samplesPath)
	enableNetworkPolicies(samplesPath)
	utils.RunMake(samplesPath, "generate", "manifests", "fmt", "vet")
}

func generateBasicOperator(path string) {
	utils.RunKubebuilderCommand(path,
		"init",
		"--domain", "olmv1.com",
		"--owner", "OLMv1 operator-framework",
	)

	// We use the deploy-image plugin because it scaffolds the full API and controller
	// logic for deploying a container image. This is especially useful for quick-start
	// Operator development, as it removes the need to manually implement reconcile logic.
	//
	// For more details, see:
	// https://book.kubebuilder.io/plugins/available/deploy-image-plugin-v1-alpha
	utils.RunKubebuilderCommand(path,
		"create", "api",
		"--group", "example",
		"--version", "v1",
		"--kind", "Busybox",
		"--image", "busybox:1.36.1",
		"--plugins", "deploy-image/v1-alpha",
	)
}

func enableNetworkPolicies(path string) {
	err := pluginutil.UncommentCode(
		filepath.Join(path, "config", "default", "kustomization.yaml"),
		"#- ../network-policy",
		"#")
	if err != nil {
		log.Fatalf("Failed to enable network policies in %s: %v", path, err)
	}
}
