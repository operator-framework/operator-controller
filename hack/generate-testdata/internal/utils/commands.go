package utils

import (
	"log"
	"os"

	"os/exec"
	"path/filepath"
)

// RunKubebuilderCommand run command with kubebuilder binary
func RunKubebuilderCommand(dir string, args ...string) {
	kbPath, err := filepath.Abs(filepath.Join("bin", "kubebuilder"))
	if err != nil {
		log.Fatalf("Failed to resolve absolute path to kubebuilder: %v", err)
	}
	cmd := exec.Command(kbPath, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Printf("Running command: %s %v", kbPath, args)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Command failed: %v", err)
	}
}

func RunMake(dir string, targets ...string) {
	for _, target := range targets {
		cmd := exec.Command("make", target)
		cmd.Dir = dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		log.Printf("Running make %s in %s", target, dir)
		if err := cmd.Run(); err != nil {
			log.Fatalf("make %s failed: %v", target, err)
		}
	}
}

// ResetSampleDir will create clean dir
func ResetSampleDir(path string) {
	if err := os.RemoveAll(path); err != nil {
		log.Fatalf("Failed to remove old sample dir: %v", err)
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		log.Fatalf("Failed to create sample dir: %v", err)
	}
}

// BuildOLMBundleRegistryV1 will build OLM bundle registry v1
// This is a temporary approach until replaced by KPM-based tooling.
func BuildOLMBundleRegistryV1(path string) {
	// TODO: Implement OLM bundle registry v1 build logic
	// The same pain that we will have to deal with it here
	// is the pain that our users, Content Authors have today,
	// PS: SDK does not work well at all. What SDK does is not
	// addressing the need and Content Authors need to create
	// scripts to build OLM bundles or change things manually
	// which is error-prone.
	// It tried to use the SDK as our users do (https://kubernetes.slack.com/archives/C0181L6JYQ2/p1748738124826539), but it is not
	// and still a lot of manual work.
}

// runOperatorSDK run command with sdk binary
func runOperatorSDK(dir string, args ...string) {
	kbPath, err := filepath.Abs(filepath.Join("bin", "operator-sdk"))
	if err != nil {
		log.Fatalf("Failed to resolve absolute path to kubebuilder: %v", err)
	}
	cmd := exec.Command(kbPath, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Printf("Running command: %s %v", kbPath, args)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Command failed: %v", err)
	}
}
