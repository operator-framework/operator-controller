# Kubernetes Staging Module Version Synchronization Tool

## Purpose
This tool ensures that if `k8s.io/kubernetes` changes version in your `go.mod`, all related staging modules (e.g., `k8s.io/api`, `k8s.io/apimachinery`) are automatically pinned to the corresponding published version.

## How It Works

1. **Parse and Filter:**
   - Reads and parses your `go.mod`.
   - Removes any existing `replace` directives referencing `k8s.io/`, ensuring no stale mappings persist.

2. **Find Kubernetes Version & Compute Staging Version:**
   - Identifies the pinned `k8s.io/kubernetes` version in the `require` block.
   - Converts `"v1.xx.yy"` to `"v0.xx.yy"` to determine the correct version for staging modules.

3. **List All Modules in the Graph:**
   - Runs `go list -m -json all` to get the full dependency tree.
   - Extracts all `k8s.io/*` modules, ensuring completeness.

4. **Pin Staging Modules:**
   - For each `k8s.io/*` module (except `k8s.io/kubernetes`):
     - If it has a `v0.0.0` version (indicating it’s untagged) or its version doesn’t match the computed published version, the tool updates the `replace` directive accordingly.
     - Ensures `k8s.io/kubernetes` itself has a `replace` entry for consistency.

5. **Write & Finalize:**
   - Writes the updated `go.mod`.
   - Runs `go mod tidy` to clean up any dangling dependencies.
   - Runs `go mod download k8s.io/kubernetes` to guarantee required entries in `go.sum`.
   - Performs a final check that no modules remain at `v0.0.0`.

## Behavior When Kubernetes Version Changes
- If you manually update `k8s.io/kubernetes` (e.g., from `v1.32.2` to `v1.32.1`) and rerun this tool:
  - The tool detects the new version and calculates the corresponding staging version (`v0.32.1`).
  - It updates all staging modules (`k8s.io/*`) to match the new version, ensuring consistency.
  - Any outdated `replace` directives are removed and replaced with the correct version.

## Additional Checks & Safeguards
- **Missing `go.sum` Entries:** If `go list -m -json all` fails due to missing entries, the tool runs `go mod download` to ensure completeness.
- **Conflicting Pinned Versions:** The tool enforces replace directives, but transitive dependencies may still cause conflicts that require manual resolution.
- **Modules Introduced/Removed in Certain Versions:** If a required module no longer exists in a given Kubernetes version, manual intervention may be needed.

## Notes
- Ensures all `k8s.io/*` modules are treated consistently, even if they were not explicitly listed in `go.mod`.
- Warns if any module remains pinned at `v0.0.0`, which could indicate an issue with upstream tagging.
