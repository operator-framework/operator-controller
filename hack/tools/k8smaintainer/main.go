package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

const k8sRepo = "k8s.io/kubernetes"

//nolint:gochecknoglobals
var goExe = "go"

func main() {
	log.SetFlags(0)
	if os.Getenv("GOEXE") != "" {
		goExe = os.Getenv("GOEXE")
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Error getting working directory: %v", err)
	}
	modRoot := findModRoot(wd)
	if modRoot == "" {
		log.Fatalf("Failed to find go.mod in %s or parent directories", wd)
	}
	if err := os.Chdir(modRoot); err != nil {
		log.Fatalf("Error changing directory to %s: %v", modRoot, err)
	}
	log.Printf("Running in module root: %s", modRoot)

	modBytes, err := os.ReadFile("go.mod")
	if err != nil {
		log.Fatalf("Error reading go.mod: %v", err)
	}

	modF, err := modfile.Parse("go.mod", modBytes, nil)
	if err != nil {
		log.Fatalf("Error parsing go.mod: %v", err)
	}

	// Find k8s.io/kubernetes version
	k8sVer := ""
	for _, req := range modF.Require {
		if req.Mod.Path == k8sRepo {
			k8sVer = req.Mod.Version
			break
		}
	}
	if k8sVer == "" {
		log.Fatalf("Could not find %s in go.mod require block", k8sRepo)
	}
	log.Printf("Found %s version: %s", k8sRepo, k8sVer)

	// Calculate target staging version
	if !semver.IsValid(k8sVer) {
		log.Fatalf("Invalid semver for %s: %s", k8sRepo, k8sVer)
	}
	majorMinor := semver.MajorMinor(k8sVer)             // e.g., "v1.32"
	patch := strings.TrimPrefix(k8sVer, majorMinor+".") // e.g., "3"
	if len(strings.Split(majorMinor, ".")) != 2 {
		log.Fatalf("Unexpected format for MajorMinor: %s", majorMinor)
	}
	targetStagingVer := "v0" + strings.TrimPrefix(majorMinor, "v1") + "." + patch // e.g., "v0.32.3"
	if !semver.IsValid(targetStagingVer) {
		log.Fatalf("Calculated invalid staging semver: %s", targetStagingVer)
	}
	log.Printf("Target staging version calculated: %s", targetStagingVer)

	// Run `go list -m -json all`
	type Module struct {
		Path    string
		Version string
		Replace *Module
		Main    bool
	}
	log.Println("Running 'go list -m -json all'...")
	output, err := runGoCommand("list", "-m", "-json", "all")
	if err != nil {
		// Try downloading first if list fails
		log.Println("go list failed, trying go mod download...")
		if _, downloadErr := runGoCommand("mod", "download"); downloadErr != nil {
			log.Fatalf("Error running 'go mod download' after list failed: %v", downloadErr)
		}
		output, err = runGoCommand("list", "-m", "-json", "all")
		if err != nil {
			log.Fatalf("Error running 'go list -m -json all' even after download: %v", err)
		}
	}

	// Iterate, identify k8s.io/* staging modules, and determine version to pin
	pins := make(map[string]string) // Module path -> version to pin
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var mod Module
		if err := decoder.Decode(&mod); err != nil {
			log.Fatalf("Error decoding go list output: %v", err)
		}

		// Skip main module, non-k8s modules, k8s.io/kubernetes itself, and versioned modules like k8s.io/client-go/v2
		_, pathSuffix, _ := module.SplitPathVersion(mod.Path) // Check if path has a version suffix like /v2, /v3 etc.
		if mod.Main || !strings.HasPrefix(mod.Path, "k8s.io/") || mod.Path == k8sRepo || pathSuffix != "" {
			continue
		}

		// Use replacement path if it exists
		effectivePath := mod.Path
		if mod.Replace != nil {
			effectivePath = mod.Replace.Path
			// Skip local file replacements
			if !strings.Contains(effectivePath, ".") { // Basic check if it looks like a module path vs local path
				log.Printf("Skipping local replace: %s => %s", mod.Path, effectivePath)
				continue
			}
		}

		// Check existence of target version, fallback to previous patch if needed
		determinedVer, err := getLatestExistingVersion(effectivePath, targetStagingVer)
		if err != nil {
			log.Printf("WARNING: Error checking versions for %s: %v. Skipping pinning.", effectivePath, err)
			continue
		}

		if determinedVer == "" {
			log.Printf("WARNING: Neither target version %s nor its predecessor found for %s. Skipping pinning.", targetStagingVer, effectivePath)
			continue
		}

		if determinedVer != targetStagingVer {
			log.Printf("WARNING: Target version %s not found for %s. Using existing version %s.", targetStagingVer, effectivePath, determinedVer)
		}

		// map the original module path (as seen in the dependency graph) to the desired version for the 'replace' directive
		pins[mod.Path] = determinedVer
	}

	// Add k8s.io/kubernetes itself to the pins map
	pins[k8sRepo] = k8sVer
	log.Printf("Identified %d k8s.io/* modules to manage.", len(pins))

	// 7. Parse go.mod again (to have a fresh modfile object)
	modBytes, err = os.ReadFile("go.mod")
	if err != nil {
		log.Fatalf("Error reading go.mod again: %v", err)
	}
	modF, err = modfile.Parse("go.mod", modBytes, nil)
	if err != nil {
		log.Fatalf("Error parsing go.mod again: %v", err)
	}

	// Remove all existing k8s.io/* replaces
	log.Println("Removing existing k8s.io/* replace directives...")
	var replacesToRemove []string
	for _, rep := range modF.Replace {
		if strings.HasPrefix(rep.Old.Path, "k8s.io/") {
			replacesToRemove = append(replacesToRemove, rep.Old.Path)
		}
	}
	for _, path := range replacesToRemove {
		if err := modF.DropReplace(path, ""); err != nil {
			// Tolerate errors if the replace was already somehow removed
			log.Printf("Note: Error dropping replace for %s (might be benign): %v", path, err)
		}
	}

	// Add new replace directives
	log.Println("Adding determined replace directives...")
	// Sort for deterministic output
	sortedPaths := make([]string, 0, len(pins))
	for path := range pins {
		sortedPaths = append(sortedPaths, path)
	}
	sort.Strings(sortedPaths)

	for _, path := range sortedPaths {
		version := pins[path]
		// Add replace for the module path itself (e.g., k8s.io/api => k8s.io/api v0.32.3)
		// This handles cases where the effective path from `go list` might differ due to other replaces
		if err := modF.AddReplace(path, "", path, version); err != nil {
			log.Fatalf("Error adding replace for %s => %s %s: %v", path, path, version, err)
		}
		log.Printf("Adding replace: %s => %s %s", path, path, version)
	}

	// Write go.mod
	log.Println("Writing updated go.mod...")
	modF.Cleanup() // Sort blocks, etc.
	newModBytes, err := modF.Format()
	if err != nil {
		log.Fatalf("Error formatting go.mod: %v", err)
	}
	if err := os.WriteFile("go.mod", newModBytes, 0600); err != nil {
		log.Fatalf("Error writing go.mod: %v", err)
	}

	// Run `go mod tidy`
	goVer := modF.Go.Version
	tidyArgs := []string{"mod", "tidy"}
	if goVer != "" {
		tidyArgs = append(tidyArgs, fmt.Sprintf("-go=%s", goVer))
		log.Printf("Running 'go mod tidy -go=%s'...", goVer)
	} else {
		log.Println("Running 'go mod tidy'...")
	}
	if _, err := runGoCommand(tidyArgs...); err != nil {
		log.Fatalf("Error running 'go mod tidy': %v", err)
	}

	// Run `go mod download k8s.io/kubernetes`
	log.Printf("Running 'go mod download %s'...", k8sRepo)
	if _, err := runGoCommand("mod", "download", k8sRepo); err != nil {
		// This might not be fatal, could be network issues, but log it prominently
		log.Printf("WARNING: Error running 'go mod download %s': %v", k8sRepo, err)
	}

	log.Println("Successfully updated k8s dependencies.")
}

// findModRoot searches for go.mod in dir and parent directories
func findModRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "" // Reached root
		}
		dir = parent
	}
}

// runGoCommand executes a go command and returns its stdout or an error
func runGoCommand(args ...string) ([]byte, error) {
	cmd := exec.Command(goExe, args...)
	cmd.Env = append(os.Environ(), "GO111MODULE=on") // Ensure module mode
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("command '%s %s' failed: %v\nStderr:\n%s", goExe, strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// getModuleVersions retrieves the list of available versions for a module
func getModuleVersions(modulePath string) ([]string, error) {
	output, err := runGoCommand("list", "-m", "-versions", modulePath)
	if err != nil {
		// Check if the error is "no matching versions" - this is not a fatal error for our logic
		if strings.Contains(string(output)+err.Error(), "no matching versions") {
			return []string{}, nil // Return empty list, not an error
		}
		return nil, fmt.Errorf("error listing versions for %s: %w", modulePath, err)
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 {
		return []string{}, nil // No versions listed
	}
	return fields[1:], nil // First field is the module path
}

// getLatestExistingVersion checks for targetVer and its predecessor, returning the latest one that exists
func getLatestExistingVersion(modulePath, targetVer string) (string, error) {
	availableVersions, err := getModuleVersions(modulePath)
	if err != nil {
		return "", err
	}

	foundTarget := false
	for _, v := range availableVersions {
		if v == targetVer {
			foundTarget = true
			break
		}
	}

	if foundTarget {
		return targetVer, nil // Target version exists
	}

	// Target not found, try previous patch version
	majorMinor := semver.MajorMinor(targetVer)                // e.g., v0.32
	patchStr := strings.TrimPrefix(targetVer, majorMinor+".") // e.g., 3
	var patch int
	if _, err := fmt.Sscan(patchStr, &patch); err != nil || patch < 1 {
		log.Printf("Could not parse patch version or patch <= 0 for %s, cannot determine predecessor.", targetVer)
		return "", nil // Cannot determine predecessor
	}
	prevPatchVer := fmt.Sprintf("%s.%d", majorMinor, patch-1) // e.g., v0.32.2

	foundPrev := false
	for _, v := range availableVersions {
		if v == prevPatchVer {
			foundPrev = true
			break
		}
	}

	if foundPrev {
		return prevPatchVer, nil // Predecessor version exists
	}

	// Neither found
	return "", nil
}
