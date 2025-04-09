package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs" // Imported for fs.FileMode
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

const (
	k8sRepo                       = "k8s.io/kubernetes"
	expectedMajorMinorParts       = 2
	goModFilename                 = "go.mod"
	goModFilePerms                = fs.FileMode(0600)
	minGoListVersionFields        = 2
	minPatchNumberToDecrementFrom = 1 // We can only decrement patch if it's 1 or greater (to get 0 or greater)
)

//nolint:gochecknoglobals
var goExe = "go"

// readAndParseGoMod reads and parses the go.mod file.
func readAndParseGoMod(filename string) ([]byte, *modfile.File, error) {
	modBytes, err := os.ReadFile(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading %s: %w", filename, err)
	}
	modF, err := modfile.Parse(filename, modBytes, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing %s: %w", filename, err)
	}
	return modBytes, modF, nil
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

	_, modF, err := readAndParseGoMod(goModFilename)
	if err != nil {
		log.Fatal(err) // Error already formatted by helper function
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
		log.Fatalf("Could not find %s in %s require block", k8sRepo, goModFilename)
	}
	log.Printf("Found %s version: %s", k8sRepo, k8sVer)

	// Calculate target staging version
	if !semver.IsValid(k8sVer) {
		log.Fatalf("Invalid semver for %s: %s", k8sRepo, k8sVer)
	}
	// Example: k8sVer = v1.32.3
	majorMinor := semver.MajorMinor(k8sVer)             // e.g., "v1.32"
	patch := strings.TrimPrefix(k8sVer, majorMinor+".") // e.g., "3"
	if len(strings.Split(majorMinor, ".")) != expectedMajorMinorParts {
		log.Fatalf("Unexpected format for MajorMinor: %s", majorMinor)
	}
	// targetStagingVer becomes "v0" + ".32" + "." + "3" => "v0.32.3"
	targetStagingVer := "v0" + strings.TrimPrefix(majorMinor, "v1") + "." + patch
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
			log.Printf("WARNING: Target version %s not found for %s. Using existing version %s.", targetStagingVer, effectivePath, determinedVer)
		}

		// map the original module path (as seen in the dependency graph) to the desired version for the 'replace' directive
		pins[mod.Path] = determinedVer
	}

	// Add k8s.io/kubernetes itself to the pins map (ensures it's covered by the replace logic)
	pins[k8sRepo] = k8sVer
	log.Printf("Identified %d k8s.io/* modules to manage.", len(pins))

	// Parse go.mod again (needed in case `go list` modified it)
	_, modF, err = readAndParseGoMod(goModFilename)
	if err != nil {
		log.Fatal(err) // Error already formatted by helper function
	}

	// Remove all existing k8s.io/* replaces
	log.Println("Removing existing k8s.io/* replace directives...")
	var replacesToRemove []string
	for _, rep := range modF.Replace {
		// Only remove replaces targeting k8s.io/* modules (not local replacements like ../staging)
		// assumes standard module paths contain '.'
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
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("command '%s %s' failed: %v\nStderr:\n%s", goExe, strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// getModuleVersions retrieves the list of available versions for a module
func getModuleVersions(modulePath string) ([]string, error) {
	output, err := runGoCommand("list", "-m", "-versions", modulePath)
	// Combine output and error message for checking because 'go list' sometimes writes errors to stdout
	combinedOutput := string(output)
	if err != nil {
		combinedOutput += err.Error()
	}

	// Check if the error/output indicates "no matching versions" - this is not a fatal error for our logic
	if strings.Contains(combinedOutput, "no matching versions") {
		return []string{}, nil // Return empty list, not an error
	}
	// If there was an actual error beyond "no matching versions"
	if err != nil {
		return nil, fmt.Errorf("error listing versions for %s: %w", modulePath, err)
	}

	fields := strings.Fields(string(output))
	if len(fields) < minGoListVersionFields {
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
	majorMinor := semver.MajorMinor(targetVer)                // e.g., v0.32
	patchStr := strings.TrimPrefix(targetVer, majorMinor+".") // e.g., 3
	var patch int
	// Use Sscan to parse the integer patch number
	if _, err := fmt.Sscan(patchStr, &patch); err != nil {
		log.Printf("Could not parse patch version from %s for module %s: %v. Cannot determine predecessor.", targetVer, modulePath, err)
		return "", nil // Cannot determine predecessor
	}

	// Only try to decrement if the patch number is >= the minimum required to do so
	if patch < minPatchNumberToDecrementFrom {
		log.Printf("Patch version %d is less than %d for %s, cannot determine predecessor.", patch, minPatchNumberToDecrementFrom, targetVer)
		return "", nil // Cannot determine predecessor (e.g., target was v0.32.0)
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
