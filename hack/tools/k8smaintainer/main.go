package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blang/semver/v4"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

const (
	k8sRepo                       = "k8s.io/kubernetes"
	expectedMajorMinorParts       = 2
	goModFilename                 = "go.mod"
	goModFilePerms                = fs.FileMode(0600)
	minGoListVersionFields        = 2
	minPatchNumberToDecrementFrom = 1 // We can only decrement patch if it's 1 or greater (to get 0 or greater)
	k8sVersionEnvVar              = "K8S_IO_K8S_VERSION"
)

//nolint:gochecknoglobals
var goExe = "go"

// readAndParseGoMod reads and parses the go.mod file.
func readAndParseGoMod(filename string) (*modfile.File, error) {
	modBytes, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %w", filename, err)
	}
	modF, err := modfile.Parse(filename, modBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("error parsing %s: %w", filename, err)
	}
	return modF, nil
}

// getK8sVersionFromEnv processes the version specified via environment variable.
// It validates the version and runs `go get` to update the dependency.
func getK8sVersionFromEnv(targetK8sVer string) (string, error) {
	log.Printf("Found target %s version override from env var %s: %s", k8sRepo, k8sVersionEnvVar, targetK8sVer)
	if _, err := semver.ParseTolerant(targetK8sVer); err != nil {
		return "", fmt.Errorf("invalid semver specified in %s: %s (%w)", k8sVersionEnvVar, targetK8sVer, err)
	}
	// Update the go.mod file first
	log.Printf("Running 'go get %s@%s' to update the main dependency...", k8sRepo, targetK8sVer)
	getArgs := fmt.Sprintf("%s@%s", k8sRepo, targetK8sVer)
	if _, err := runGoCommand("get", getArgs); err != nil {
		return "", fmt.Errorf("error running 'go get %s': %w", getArgs, err)
	}
	return targetK8sVer, nil // Return the validated version
}

// getK8sVersionFromMod reads the go.mod file to find the current version of k8s.io/kubernetes.
// It returns the version string if found, or an empty string (and nil error) if not found.
func getK8sVersionFromMod() (string, error) {
	modF, err := readAndParseGoMod(goModFilename)
	if err != nil {
		return "", err // Propagate error from reading/parsing
	}

	// Find k8s.io/kubernetes version
	for _, req := range modF.Require {
		if req.Mod.Path == k8sRepo {
			log.Printf("Found existing %s version in %s: %s", k8sRepo, goModFilename, req.Mod.Version)
			return req.Mod.Version, nil // Return found version
		}
	}
	// Not found case - return empty string, no error (as per original logic)
	log.Printf("INFO: %s not found in %s require block. Nothing to do.", k8sRepo, goModFilename)
	return "", nil
}

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
		log.Fatalf("Failed to find %s in %s or parent directories", goModFilename, wd)
	}
	if err := os.Chdir(modRoot); err != nil {
		log.Fatalf("Error changing directory to %s: %v", modRoot, err)
	}
	log.Printf("Running in module root: %s", modRoot)

	var k8sVer string

	// Determine the target k8s version using helper functions
	targetK8sVerEnv := os.Getenv(k8sVersionEnvVar)
	if targetK8sVerEnv != "" {
		// Process version from environment variable
		k8sVer, err = getK8sVersionFromEnv(targetK8sVerEnv)
		if err != nil {
			log.Fatalf("Failed to process k8s version from environment variable %s: %v", k8sVersionEnvVar, err)
		}
	} else {
		// Process version from go.mod file
		k8sVer, err = getK8sVersionFromMod()
		if err != nil {
			log.Fatalf("Failed to get k8s version from %s: %v", goModFilename, err)
		}
		// Handle the "not found" case where getK8sVersionFromMod returns "", nil
		if k8sVer == "" {
			os.Exit(0) // Exit gracefully as requested
		}
	}

	// Calculate target staging version
	k8sSemVer, err := semver.ParseTolerant(k8sVer)
	if err != nil {
		// This should ideally not happen if validation passed earlier, but check anyway.
		log.Fatalf("Invalid semver for %s: %s (%v)", k8sRepo, k8sVer, err) // Adjusted log format slightly
	}

	if k8sSemVer.Major != 1 {
		log.Fatalf("Expected k8s version %s to have major version 1", k8sVer)
	}
	targetSemVer := semver.Version{Major: 0, Minor: k8sSemVer.Minor, Patch: k8sSemVer.Patch}
	// Prepend 'v' as expected by Go modules and the rest of the script logic
	targetStagingVer := "v" + targetSemVer.String()

	// Validate the constructed staging version string
	if _, err := semver.ParseTolerant(targetStagingVer); err != nil {
		log.Fatalf("Calculated invalid staging semver: %s from k8s version %s (%v)", targetStagingVer, k8sVer, err) // Adjusted log format slightly
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
		log.Println("'go list' failed, trying 'go mod download'...")
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

		// Use replacement path if it exists, but skip local file replacements
		effectivePath := mod.Path
		if mod.Replace != nil {
			// Heuristic: Assume module paths have a domain-like structure (e.g., 'xxx.yyy/zzz') in the first segment.
			// Local paths usually don't (e.g., '../othermod', './local').
			parts := strings.SplitN(mod.Replace.Path, "/", 2)
			if len(parts) > 0 && !strings.Contains(parts[0], ".") {
				log.Printf("Skipping local replace: %s => %s", mod.Path, mod.Replace.Path)
				continue
			}
			effectivePath = mod.Replace.Path
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
			log.Printf("INFO: Target version %s not found for %s. Using existing predecessor version %s.", targetStagingVer, effectivePath, determinedVer)
		}

		// map the original module path (as seen in the dependency graph) to the desired version for the 'replace' directive
		pins[mod.Path] = determinedVer
	}

	// Add k8s.io/kubernetes itself to the pins map (ensures it's covered by the replace logic)
	pins[k8sRepo] = k8sVer
	log.Printf("Identified %d k8s.io/* modules to manage.", len(pins))

	// Parse go.mod again (needed in case `go list` or `go get` modified it)
	modF, err := readAndParseGoMod(goModFilename)
	if err != nil {
		log.Fatal(err) // Error already formatted by helper function
	}

	// Remove all existing k8s.io/* replaces that target other modules (not local paths)
	log.Println("Removing existing k8s.io/* module replace directives...")
	var replacesToRemove []string
	for _, rep := range modF.Replace {
		// Only remove replaces targeting k8s.io/* modules (not local replacements like ../staging)
		// Check that the old path starts with k8s.io/ and the new path looks like a module path (contains '.')
		if strings.HasPrefix(rep.Old.Path, "k8s.io/") && strings.Contains(rep.New.Path, ".") {
			replacesToRemove = append(replacesToRemove, rep.Old.Path)
		} else if strings.HasPrefix(rep.Old.Path, "k8s.io/") {
			log.Printf("Note: Found existing non-module replace for %s, leaving untouched: %s => %s %s", rep.Old.Path, rep.Old.Path, rep.New.Path, rep.New.Version)
		}
	}
	if len(replacesToRemove) > 0 {
		for _, path := range replacesToRemove {
			log.Printf("Removing replace for: %s", path)
			// Drop replace expects oldPath and oldVersion. Version is empty for path-only replaces.
			if err := modF.DropReplace(path, ""); err != nil {
				// Tolerate errors if the replace was already somehow removed or structure changed
				log.Printf("Note: Error dropping replace for %s (might be benign): %v", path, err)
			}
		}
	} else {
		log.Println("No existing k8s.io/* module replaces found to remove.")
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
		if err := modF.AddReplace(path, "", path, version); err != nil {
			log.Fatalf("Error adding replace for %s => %s %s: %v", path, path, version, err)
		}
		log.Printf("Adding replace: %s => %s %s", path, path, version)
	}

	// Write go.mod
	log.Println("Writing updated go.mod...")
	modF.Cleanup() // Sort blocks, remove redundant directives etc.
	newModBytes, err := modF.Format()
	if err != nil {
		log.Fatalf("Error formatting go.mod: %v", err)
	}
	if err := os.WriteFile(goModFilename, newModBytes, goModFilePerms); err != nil {
		log.Fatalf("Error writing %s: %v", goModFilename, err)
	}

	// Run `go mod tidy`
	goVer := ""
	if modF.Go != nil { // Ensure Go directive exists before accessing Version
		goVer = modF.Go.Version
	}
	tidyArgs := []string{"mod", "tidy"}
	if goVer != "" {
		tidyArgs = append(tidyArgs, fmt.Sprintf("-go=%s", goVer))
	}
	log.Printf("Running '%s %s'...", goExe, strings.Join(tidyArgs, " "))
	if _, err := runGoCommand(tidyArgs...); err != nil {
		log.Fatalf("Error running 'go mod tidy': %v", err)
	}

	// Run `go mod download k8s.io/kubernetes`
	log.Printf("Running '%s mod download %s'...", goExe, k8sRepo)
	if _, err := runGoCommand("mod", "download", k8sRepo); err != nil {
		// This might not be fatal, could be network issues, but log it prominently
		log.Printf("WARNING: Error running 'go mod download %s': %v", k8sRepo, err)
	}

	log.Println("Successfully updated k8s dependencies.")
}

// findModRoot searches for go.mod in dir and parent directories
func findModRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, goModFilename)); err == nil {
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
	log.Printf("Executing: %s %s", goExe, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			log.Printf("Stderr:\n%s", stderr.String())
		}
		return nil, fmt.Errorf("command '%s %s' failed: %w", goExe, strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

// getModuleVersions retrieves the list of available versions for a module
func getModuleVersions(modulePath string) ([]string, error) {
	output, err := runGoCommand("list", "-m", "-versions", modulePath)
	// Combine output and error message for checking because 'go list' sometimes writes errors to stdout
	combinedOutput := string(output)
	if err != nil {
		if !strings.Contains(combinedOutput, err.Error()) {
			combinedOutput += err.Error()
		}
	}

	// Check if the error/output indicates "no matching versions" - this is not a fatal error for our logic
	if strings.Contains(combinedOutput, "no matching versions") || strings.Contains(combinedOutput, "no required module provides package") {
		log.Printf("INFO: No versions found for module %s via 'go list'.", modulePath)
		return []string{}, nil // Return empty list, not an error
	}
	// If there was an actual error beyond "no matching versions"
	if err != nil {
		return nil, fmt.Errorf("error listing versions for %s: %w", modulePath, err)
	}

	fields := strings.Fields(string(output))
	if len(fields) < minGoListVersionFields {
		log.Printf("INFO: No versions listed for module %s (output: '%s')", modulePath, string(output))
		return []string{}, nil // No versions listed (e.g., just the module path)
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
	targetSemVer, err := semver.ParseTolerant(targetVer)
	if err != nil {
		log.Printf("Could not parse target version %s for module %s: %v. Cannot determine predecessor.", targetVer, modulePath, err)
		return "", nil // Cannot determine predecessor
	}

	// Only try to decrement if the patch number is >= the minimum required to do so
	if targetSemVer.Patch < uint64(minPatchNumberToDecrementFrom) {
		log.Printf("Patch version %d is less than %d for %s, cannot determine predecessor.", targetSemVer.Patch, minPatchNumberToDecrementFrom, targetVer)
		return "", nil // Cannot determine predecessor (e.g., target was v0.32.0)
	}

	prevSemVer := targetSemVer
	prevSemVer.Patch--
	prevPatchVer := "v" + prevSemVer.String()

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
