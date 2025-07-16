package samples

import (
	"fmt"
	"github.com/operator-framework/operator-controller/hack/generate-testdata/internal/utils"
	"path/filepath"
	pluginutil "sigs.k8s.io/kubebuilder/v4/pkg/plugin/util"
)

// BuildSampleV2 generate sample v2.0.0 with breaking changes
// ---------------------------------------------
// This version introduces a new API version with webhook conversion.
// on top of v1.0.0 version.
func BuildSampleV2(samplesPath string) {
	BuildSampleV1(samplesPath)

	// Create v2 API version without controller
	utils.RunKubebuilderCommand(samplesPath,
		"create", "api",
		"--group", "example",
		"--version", "v2",
		"--kind", "Busybox",
		"--resource",
		"--controller=false",
	)

	// Create conversion webhook for Busybox v1 -> v2
	// Create webhook with defaulting and validation
	utils.RunKubebuilderCommand(samplesPath,
		"create", "webhook",
		"--group", "example",
		"--version", "v1",
		"--kind", "Busybox",
		"--conversion",
		"--programmatic-validation",
		"--defaulting",
		"--spoke", "v2",
	)

	implementV2Type(samplesPath)
	implementV2WebhookConversion(samplesPath)
	implementValidationDefaultWebhookV1(samplesPath)

	utils.RunMake(samplesPath, "generate", "manifests", "fmt", "vet")
}

func implementV2Type(path string) {
	fmt.Println("Adding `Replicas` field to Busybox v2 spec")
	v2TypesPath := filepath.Join(path, "api", "v2", "busybox_types.go")
	if err := pluginutil.ReplaceInFile(
		v2TypesPath,
		"Foo *string `json:\"foo,omitempty\"`",
		"Replicas *int32 `json:\"replicas,omitempty\"` // Number of replicas",
	); err != nil {
		panic(fmt.Sprintf("failed to insert replicas field in v2 BusyboxSpec: %v", err))
	}
}

func implementV2WebhookConversion(path string) {
	fmt.Println("Implementing conversion logic for v2 <-> v1")
	conversionPath := filepath.Join(path, "api", "v2", "busybox_conversion.go")
	if err := pluginutil.UncommentCode(
		conversionPath,
		"// dst.Spec.Size = src.Spec.Replicas",
		"//",
	); err != nil {
		panic(fmt.Sprintf("failed to implement v2->v1 conversion: %v", err))
	}

	if err := pluginutil.UncommentCode(
		conversionPath,
		"// dst.Spec.Replicas = src.Spec.Size",
		"//",
	); err != nil {
		panic(fmt.Sprintf("failed to implement v1->v2 conversion: %v", err))
	}
}

// implementValidationDefaultWebhookV1 injects validation and defaulting logic into the Busybox v1 webhook.
func implementValidationDefaultWebhookV1(samplesPath string) {
	webhookPath := filepath.Join(samplesPath, "internal", "webhook", "v1", "busybox_webhook.go")

	fmt.Println("Injecting validation logic into Busybox v1 webhook")
	// ValidateCreate logic
	if err := pluginutil.ReplaceInFile(
		webhookPath,
		"// TODO(user): fill in your validation logic upon object creation.",
		`if busybox.Spec.Size != nil && *busybox.Spec.Size < 0 {
		return nil, fmt.Errorf("spec.size must be >= 0")
	}`,
	); err != nil {
		panic(fmt.Sprintf("failed to apply ValidateCreate logic: %v", err))
	}

	// ValidateUpdate logic
	if err := pluginutil.ReplaceInFile(
		webhookPath,
		"// TODO(user): fill in your validation logic upon object update.",
		`if busybox.Spec.Size != nil && *busybox.Spec.Size < 0 {
		return nil, fmt.Errorf("spec.size must be >= 0")
	}`,
	); err != nil {
		panic(fmt.Sprintf("failed to apply ValidateUpdate logic: %v", err))
	}
}
