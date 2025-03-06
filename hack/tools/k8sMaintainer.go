package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

// debug controls whether we print extra statements.
var debug = true

// moduleInfo is the partial output of `go list -m -json all`.
type moduleInfo struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
}

func main() {
	if err := fixGoMod("go.mod"); err != nil {
		fmt.Fprintf(os.Stderr, "fixGoMod failed: %v\n", err)
		os.Exit(1)
	}
}

// fixGoMod is the main entrypoint. It does a 2‐phase approach:
//
//	Remove old k8s.io/* replace lines, rewrite + tidy so they’re really gone.
//	Parse again, unify staging modules in require + replace to the new patch version, rewrite + tidy.
func fixGoMod(goModPath string) error {
	// parse & remove old lines, write, go mod tidy
	mf1, err := parseMod(goModPath)
	if err != nil {
		return err
	}
	pruneK8sReplaces(mf1)
	mf1.SortBlocks()
	mf1.Cleanup()

	if err := writeModFile(mf1, goModPath); err != nil {
		return err
	}
	if err := runCmd("go", "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}
	//parse again, unify everything to derived patch version in both require + replace blocks, write, go mod tidy
	mf2, err := parseMod(goModPath)
	if err != nil {
		return err
	}

	k8sVer := findKubernetesVersion(mf2)
	if k8sVer == "" {
		return fmt.Errorf("did not find k8s.io/kubernetes in require block")
	}
	fmt.Printf("Found k8s.io/kubernetes version: %s\n", k8sVer)

	published := toPublishedVersion(k8sVer)
	if published == "" {
		return fmt.Errorf("cannot derive staging version from %s", k8sVer)
	}
	fmt.Printf("Unifying staging modules to: %s (from %s)\n", published, k8sVer)

	// forcibly unify the REQUIRE items for all staging modules
	forceRequireStaging(mf2, published)

	// discover all k8s.io/* modules in the graph and unify them with new replace lines
	listOut, errOut, err := runGoList()
	if err != nil {
		return fmt.Errorf("go list: %v\nStderr:\n%s", err, errOut)
	}
	stagingPins := discoverPinsAlways(listOut, published)
	applyReplacements(mf2, stagingPins)

	// also ensure we have a replace for k8s.io/kubernetes => same version
	ensureKubernetesReplace(mf2, k8sVer)

	mf2.SortBlocks()
	mf2.Cleanup()

	if err := writeModFile(mf2, goModPath); err != nil {
		return err
	}
	if err := runCmd("go", "mod", "tidy"); err != nil {
		return fmt.Errorf("final tidy failed: %w", err)
	}
	if err := runCmd("go", "mod", "download", "k8s.io/kubernetes"); err != nil {
		return fmt.Errorf("final: go mod download k8s.io/kubernetes failed: %w", err)
	}

	// final check
	finalOut, err := exec.Command("go", "list", "-m", "all").Output()
	if err != nil {
		return fmt.Errorf("running final go list: %w", err)
	}
	if bytes.Contains(finalOut, []byte("v0.0.0")) {
		fmt.Println("Warning: Some modules remain at v0.0.0, possibly no valid tags.")
	} else {
		fmt.Println("Success: staging modules pinned to", published)
	}
	return nil
}

// parseMod reads go.mod into memory as a modfile.File
func parseMod(path string) (*modfile.File, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	mf, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return mf, nil
}

// writeModFile formats and writes the modfile back to disk
func writeModFile(mf *modfile.File, path string) error {
	formatted, err := mf.Format()
	if err != nil {
		return fmt.Errorf("formatting modfile: %w", err)
	}
	if err := ioutil.WriteFile(path, formatted, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if debug {
		fmt.Printf("Wrote %s\n", path)
	}
	return nil
}

// pruneK8sReplaces removes any replace lines with Old.Path prefix "k8s.io/"
func pruneK8sReplaces(mf *modfile.File) {
	var keep []*modfile.Replace
	for _, rep := range mf.Replace {
		if strings.HasPrefix(rep.Old.Path, "k8s.io/") {
			fmt.Printf("Dropping old replace for %s => %s %s\n",
				rep.Old.Path, rep.New.Path, rep.New.Version)
		} else {
			keep = append(keep, rep)
		}
	}
	mf.Replace = keep
}

// forceRequireStaging forcibly sets the require lines for all staging modules
// (k8s.io/*) to the desired patch version if a valid tag is found. We remove
// the old line first, then AddRequire so the final go.mod show them updated.
func forceRequireStaging(mf *modfile.File, newVersion string) {
	var stagingPaths []string

	// gather all relevant require lines we want to unify
	for _, req := range mf.Require {
		p := req.Mod.Path
		if strings.HasPrefix(p, "k8s.io/") &&
			p != "k8s.io/kubernetes" &&
			!hasMajorVersionSuffix(p) {
			stagingPaths = append(stagingPaths, p)
		}
	}
	// remove them
	for _, p := range stagingPaths {
		fmt.Printf("Removing require line for %s\n", p)
		_ = mf.DropRequire(p) // returns an error if not found, ignore
	}
	// re-add them at the new version if we can download that version
	for _, p := range stagingPaths {
		if versionExists(p, newVersion) {
			fmt.Printf("Adding require line for %s at %s\n", p, newVersion)
			_ = mf.AddRequire(p, newVersion)
		} else {
			fmt.Printf("WARNING: no valid tag for %s at %s, skipping\n", p, newVersion)
		}
	}
}

// discoverPinsAlways identifies k8s.io/* modules from the "go list -m -json all"
// output, and unifies them all to `published` if it’s downloadable. This does
// not skip forced downgrades. If it's a staging path, we pin it.
func discoverPinsAlways(listOut, published string) map[string]string {
	pins := make(map[string]string)
	dec := json.NewDecoder(strings.NewReader(listOut))
	for {
		var mi moduleInfo
		if err := dec.Decode(&mi); err != nil {
			break
		}
		if !strings.HasPrefix(mi.Path, "k8s.io/") {
			continue
		}
		if mi.Path == "k8s.io/kubernetes" {
			continue
		}
		if hasMajorVersionSuffix(mi.Path) {
			fmt.Printf("Skipping major-version module %s\n", mi.Path)
			continue
		}
		// unify everything if a valid tag exists
		if mi.Version != published {
			if versionExists(mi.Path, published) {
				fmt.Printf("Pinning %s from %s to %s\n", mi.Path, mi.Version, published)
				pins[mi.Path] = published
			} else {
				fmt.Printf("WARNING: no valid tag for %s at %s, leaving as %s\n",
					mi.Path, published, mi.Version)
			}
		}
	}
	return pins
}

// applyReplacements adds replace lines for each pinned staging module
func applyReplacements(mf *modfile.File, pins map[string]string) {
	if len(pins) == 0 {
		return
	}
	var sorted []string
	for p := range pins {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	for _, path := range sorted {
		ver := pins[path]
		fmt.Printf("Applying new replace: %s => %s\n", path, ver)
		if err := mf.AddReplace(path, "", path, ver); err != nil {
			die("Error adding replace for %s: %v", path, err)
		}
	}
}

// ensureKubernetesReplace ensures there's a "k8s.io/kubernetes => k8s.io/kubernetes vX.Y.Z" line
// matching the require(...) version in case something references it directly.
func ensureKubernetesReplace(mf *modfile.File, k8sVer string) {
	found := false
	for _, rep := range mf.Replace {
		if rep.Old.Path == "k8s.io/kubernetes" {
			found = true
			if rep.New.Version != k8sVer {
				fmt.Printf("Updating k8s.io/kubernetes replace from %s to %s\n",
					rep.New.Version, k8sVer)
				rep.New.Version = k8sVer
			}
			break
		}
	}
	if !found {
		fmt.Printf("Inserting k8s.io/kubernetes => %s\n", k8sVer)
		if err := mf.AddReplace("k8s.io/kubernetes", "", "k8s.io/kubernetes", k8sVer); err != nil {
			die("Error adding replace for k8s.io/kubernetes: %v", err)
		}
	}
}

// findKubernetesVersion returns the version in the require(...) block for k8s.io/kubernetes
func findKubernetesVersion(mf *modfile.File) string {
	for _, req := range mf.Require {
		if req.Mod.Path == "k8s.io/kubernetes" {
			return req.Mod.Version
		}
	}
	return ""
}

// toPublishedVersion: e.g. "v1.32.2" => "v0.32.2"
func toPublishedVersion(k8sVersion string) string {
	if !strings.HasPrefix(k8sVersion, "v") {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(k8sVersion, "v"), ".")
	if len(parts) < 3 {
		return ""
	}
	return fmt.Sprintf("v0.%s.%s", parts[1], parts[2])
}

// runGoList runs "go list -m -json all" and returns stdout, stderr, error
func runGoList() (string, string, error) {
	cmd := exec.Command("go", "list", "-m", "-json", "all")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// runCmd runs a command with stdout/stderr displayed. Returns an error if it fails.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// versionExists quietly tries `go mod download modPath@ver`. If 0 exit code => true.
func versionExists(modPath, ver string) bool {
	cmd := exec.Command("go", "mod", "download", fmt.Sprintf("%s@%s", modPath, ver))
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// hasMajorVersionSuffix checks for trailing /v2, /v3, etc. in the module path
func hasMajorVersionSuffix(path string) bool {
	segs := strings.Split(path, "/")
	last := segs[len(segs)-1]
	return len(last) > 1 && last[0] == 'v' && last[1] >= '2' && last[1] <= '9'
}

// die prints an error and exits.
func die(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
