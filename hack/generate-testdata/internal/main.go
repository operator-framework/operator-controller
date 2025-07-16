package main

import (
	"github.com/operator-framework/operator-controller/hack/generate-testdata/internal/samples"
	"github.com/operator-framework/operator-controller/hack/generate-testdata/internal/utils"
	"log"
	"os"
	"path/filepath"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	
	// TODO: Use cobra to allow pass the path to the testdata directory
	samplesPath := filepath.Join(wd, "./../../testdata/operators")
	log.Printf("Writing sample directories under: %s", samplesPath)

	// Remove all previous sample output before regenerating
	if err := os.RemoveAll(samplesPath); err != nil {
		log.Fatalf("failed to clean testdata/operators: %v", err)
	}

	// Sample v1.0.0 — Basic Operator with one API and controller to deploy and manage a simple workload.
	pathV1 := filepath.Join(samplesPath, "v1.0.0", "test-operator")
	utils.ResetSampleDir(pathV1)
	samples.BuildSampleV1(pathV1)
	utils.BuildOLMBundleRegistryV1(pathV1)

	// Sample v2.0.0 — Introduce a new API version with webhook conversion
	// And enable NetworkPolicy
	pathV2 := filepath.Join(samplesPath, "v2.0.0", "test-operator")
	utils.ResetSampleDir(pathV2)
	samples.BuildSampleV2(pathV2)
	utils.BuildOLMBundleRegistryV1(pathV2)
}
