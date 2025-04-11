# Kubernetes Staging Module Version Synchronization Tool

## Purpose
This tool ensures that if `k8s.io/kubernetes` changes version in your `go.mod`, all related staging modules (e.g., `k8s.io/api`, `k8s.io/apimachinery`) are automatically pinned to the corresponding published version. Recent improvements include an environment variable override and refined logic for version resolution.

## How It Works

1. **Parsing and Filtering:**
    - Reads and parses your `go.mod` file.
    - Removes existing `replace` directives for `k8s.io/` modules to avoid stale mappings.

2. **Determine Kubernetes Version:**
    - **Environment Variable Override:**  
      If the environment variable `K8S_IO_K8S_VERSION` is set, its value is validated (using semver standards) and used as the target version for `k8s.io/kubernetes`. The tool then runs `go get k8s.io/kubernetes@<version>` to update the dependency.
    - **Default Behavior:**  
      If `K8S_IO_K8S_VERSION` is not set, the tool reads the version of `k8s.io/kubernetes` from the `go.mod` file.

3. **Compute the Target Staging Version:**
    - Converts a Kubernetes version in the form `v1.xx.yy` into the staging version format `v0.xx.yy`.
    - If the target staging version is unavailable, the tool attempts to fall back to the previous patch version.

4. **Updating Module Replace Directives:**
    - Retrieves the full dependency graph using `go list -m -json all`.
    - Identifies relevant `k8s.io/*` modules (skipping the main module and version-suffixed modules).
    - Removes outdated `replace` directives (ignoring local path replacements).
    - Adds new `replace` directives to pin modules—including `k8s.io/kubernetes`—to the computed staging version.

5. **Finalizing Changes:**
    - Writes the updated `go.mod` file.
    - Runs `go mod tidy` to clean up dependencies.
    - Executes `go mod download k8s.io/kubernetes` to update `go.sum`.
    - Logs any issues, such as modules remaining at an untagged version (`v0.0.0`), which may indicate upstream tagging problems.

## Environment Variables

- **K8S_IO_K8S_VERSION (optional):**  
  When set, this environment variable overrides the Kubernetes version found in `go.mod`. The tool validates this semver string, updates the dependency using `go get`, and processes modules accordingly.

## Additional Notes

- The tool ensures consistency across all `k8s.io/*` modules, even if they are not explicitly listed in `go.mod`.
- If a suitable staging version is not found, a warning is logged and the closest valid version is used.
- All operations are logged, which helps in troubleshooting and verifying the process.