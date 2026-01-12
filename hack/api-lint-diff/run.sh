#!/usr/bin/env bash

set -euo pipefail

# Colors for output
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Temporary directory for this run
TEMP_DIR=""
WORKTREE_DIR=""
BASELINE_BRANCH="${BASELINE_BRANCH:-main}"
API_DIR="api"

# Cleanup function
cleanup() {
    # Clean up git worktree first if it exists
    if [[ -n "${WORKTREE_DIR}" && -d "${WORKTREE_DIR}" ]]; then
        git worktree remove "${WORKTREE_DIR}" --force &> /dev/null || true
    fi

    # Clean up temporary directory
    if [[ -n "${TEMP_DIR}" && -d "${TEMP_DIR}" ]]; then
        rm -rf "${TEMP_DIR}"
    fi
}

trap cleanup EXIT

# Ensure we're in the repository root
if [[ ! -d ".git" ]]; then
    echo -e "${RED}Error: Must be run from repository root${NC}"
    exit 1
fi

if [[ ! -d "${API_DIR}" ]]; then
    echo -e "${RED}Error: ${API_DIR}/ directory not found${NC}"
    exit 1
fi

# Create temporary directory
TEMP_DIR=$(mktemp -d)
echo -e "${BLUE}Using temporary directory: ${TEMP_DIR}${NC}" >&2

# Get golangci-lint version from bingo
get_golangci_version() {
    # Extract version from .bingo/Variables.mk
    # Format: GOLANGCI_LINT := $(GOBIN)/golangci-lint-v2.7.2
    local version
    version=$(grep 'GOLANGCI_LINT :=' .bingo/Variables.mk 2>/dev/null | sed -E 's/.*golangci-lint-(v[0-9.]+).*/\1/')

    # Validate that we got a version
    if [[ -z "${version}" ]]; then
        echo -e "${YELLOW}Warning: Could not extract golangci-lint version from .bingo/Variables.mk${NC}" >&2
        echo -e "${YELLOW}Using default version: latest${NC}" >&2
        version="latest"
    fi

    echo "${version}"
}

# Create temporary golangci-lint config for kube-api-linter
# This config focuses only on kube-api-linter for the api/ directory
create_temp_config() {
    cat > "${TEMP_DIR}/.golangci.yaml" <<'EOF'
version: "2"
output:
  formats:
    tab:
      path: stdout
      colors: false
linters:
  enable:
    - kubeapilinter
  settings:
    custom:
      kubeapilinter:
        type: module
        description: "Kube API Linter plugin"
        original-url: "sigs.k8s.io/kube-api-linter"
        settings:
          linters: {}
          lintersConfig:
            optionalfields:
              pointers:
                preference: WhenRequired
run:
  timeout: 5m

issues:
  exclude-rules:
    # Ignore generated files
    - path: zz_generated\..*\.go
      linters:
        - kubeapilinter
  max-issues-per-linter: 0
  max-same-issues: 0
EOF
}

# Get kube-api-linter version - pinned for supply chain security
get_kube_api_linter_version() {
    # Pin to specific pseudo-version to avoid supply chain risks
    # kube-api-linter doesn't have semantic version tags, so we use a pseudo-version
    # Update this version intentionally as part of dependency management
    # To update: GOPROXY=https://proxy.golang.org go list -m -json sigs.k8s.io/kube-api-linter@latest
    local version="v0.0.0-20251219161032-180d2bd496ef"  # Latest as of 2025-12-19

    echo "${version}"
}

# Create custom golangci-lint configuration
create_custom_gcl_config() {
    # Get golangci-lint version from bingo
    local golangci_version
    golangci_version=$(get_golangci_version)

    # Validate version is not empty
    if [[ -z "${golangci_version}" ]]; then
        echo -e "${RED}Error: Failed to determine golangci-lint version from .bingo/Variables.mk${NC}" >&2
        exit 1
    fi

    # Get kube-api-linter version (pinned for supply chain security)
    local kube_api_linter_version
    kube_api_linter_version=$(get_kube_api_linter_version)

    # Create custom-gcl config
    cat > "${TEMP_DIR}/.custom-gcl.yml" <<EOF
version: ${golangci_version}
name: golangci-lint-kube-api
destination: ${TEMP_DIR}/bin

plugins:
  - module: sigs.k8s.io/kube-api-linter
    version: ${kube_api_linter_version}
EOF
}

# Build custom golangci-lint with kube-api-linter plugin
build_custom_linter() {
    local base_linter="$1"
    local custom_binary="${TEMP_DIR}/bin/golangci-lint-kube-api"

    echo -e "${BLUE}Building custom golangci-lint with kube-api-linter plugin...${NC}" >&2

    # Create custom config
    create_custom_gcl_config

    # Build custom golangci-lint using the 'custom' command
    # This requires the base golangci-lint binary and runs from TEMP_DIR
    # where .custom-gcl.yml is located
    echo -e "${BLUE}Running golangci-lint custom build...${NC}" >&2
    local build_output
    local abs_base_linter
    # Convert to absolute path
    if [[ "${base_linter}" != /* ]]; then
        abs_base_linter="$(pwd)/${base_linter}"
    else
        abs_base_linter="${base_linter}"
    fi

    if ! build_output=$(cd "${TEMP_DIR}" && "${abs_base_linter}" custom -v 2>&1); then
        echo -e "${YELLOW}Warning: Failed to build custom golangci-lint${NC}" >&2
        echo -e "${YELLOW}Build output:${NC}" >&2
        echo "${build_output}" >&2
        echo -e "${YELLOW}Falling back to base linter (kube-api-linter will not be available)${NC}" >&2
        return 1
    fi
    echo -e "${BLUE}Custom linter build completed${NC}" >&2

    if [[ -f "${custom_binary}" ]]; then
        echo -e "${GREEN}Successfully built custom golangci-lint at ${custom_binary}${NC}" >&2
        # Only echo the binary path to stdout for capture
        echo "${custom_binary}"
        return 0
    else
        echo -e "${YELLOW}Warning: Custom binary not found at expected location${NC}" >&2
        return 1
    fi
}

# Function to check if golangci-lint has kube-api-linter
check_linter_support() {
    local linter_path="$1"
    if ! "${linter_path}" linters 2>/dev/null | grep -q "kubeapilinter"; then
        echo -e "${YELLOW}Warning: golangci-lint at ${linter_path} does not have kubeapilinter plugin${NC}"
        echo -e "${YELLOW}Linting results may be incomplete. Consider using custom golangci-lint build.${NC}"
        return 1
    fi
    return 0
}

# Find golangci-lint binary
find_golangci_lint() {
    # Check if variables.env exists and extract golangci-lint path
    if [[ -f ".bingo/variables.env" ]]; then
        source .bingo/variables.env 2>/dev/null || true
        if [[ -n "${GOLANGCI_LINT}" && -f "${GOLANGCI_LINT}" ]]; then
            echo "${GOLANGCI_LINT}"
            return 0
        fi
    fi

    # Check for custom build
    if [[ -f ".bingo/golangci-lint" ]]; then
        echo ".bingo/golangci-lint"
        return 0
    fi

    # Check for bin/golangci-lint
    if [[ -f "bin/golangci-lint" ]]; then
        echo "bin/golangci-lint"
        return 0
    fi

    # Fall back to system golangci-lint
    if command -v golangci-lint &> /dev/null; then
        echo "golangci-lint"
        return 0
    fi

    echo -e "${RED}Error: golangci-lint not found.${NC}" >&2
    echo -e "${RED}Searched for:${NC}" >&2
    echo -e "  - .bingo/variables.env (bingo-managed variables for GOLANGCI_LINT)" >&2
    echo -e "  - .bingo/golangci-lint" >&2
    echo -e "  - bin/golangci-lint" >&2
    echo -e "  - golangci-lint on your \$PATH" >&2
    exit 1
}

# Run linter and capture output
run_linter() {
    local config_file="$1"
    local output_file="$2"
    local linter_path="$3"
    local repo_root="${4:-$(pwd)}"

    # Run golangci-lint on api/ directory only
    # Use absolute paths to ensure consistency
    (cd "${repo_root}" && "${linter_path}" run \
        --config="${config_file}" \
        --path-prefix="" \
        ./api/...) > "${output_file}" 2>&1 || true
}

# Parse linter output into structured format
# Format: filename:line:column:linter:message
parse_linter_output() {
    local output_file="$1"
    local parsed_file="$2"

    # Expected format: path/api/v1/file.go:123:45  <spaces>  linter  <spaces>  message
    # We need to: extract api/ relative path, parse line:col, linter, and message
    grep "/${API_DIR}/" "${output_file}" | \
        sed -E "s|^.*/("${API_DIR}"/[^:]+):([0-9]+):([0-9]+)[[:space:]]+([^[:space:]]+)[[:space:]]+(.+)$|\1:\2:\3:\4:\5|" \
        > "${parsed_file}" || true
}

# Get list of files changed in api/ directory compared to baseline
get_changed_files() {
    git diff "${BASELINE_BRANCH}...HEAD" --name-only -- "${API_DIR}/" | \
        grep '\.go$' | \
        grep -v 'zz_generated' || true
}

# Categorize issues as NEW, PRE-EXISTING, or FIXED
categorize_issues() {
    local current_file="$1"
    local baseline_file="$2"
    local changed_files_file="$3"
    local new_issues_file="$4"
    local preexisting_issues_file="$5"
    local fixed_issues_file="$6"

    # Read changed files into array
    local changed_files=()
    if [[ -f "${changed_files_file}" ]]; then
        while IFS= read -r file; do
            changed_files+=("${file}")
        done < "${changed_files_file}"
    fi

    # Process current issues only if file exists and is not empty
    if [[ -f "${current_file}" && -s "${current_file}" ]]; then
        while IFS= read -r line; do
            [[ -z "${line}" ]] && continue

            local file
            file=$(echo "${line}" | cut -d: -f1)

            # If no files were changed, all issues are pre-existing
            if [[ ${#changed_files[@]} -eq 0 ]]; then
                echo "${line}" >> "${preexisting_issues_file}"
                continue
            fi

            # Check if file was changed
            local file_changed=false
            for changed_file in "${changed_files[@]}"; do
                if [[ "${file}" == "${changed_file}" ]]; then
                    file_changed=true
                    break
                fi
            done

            # If file wasn't changed, it's pre-existing
            if ! $file_changed; then
                echo "${line}" >> "${preexisting_issues_file}"
                continue
            fi

            # Check if issue exists in baseline
            # Compare without line numbers since line numbers can change when code is added/removed
            # Format is: file:line:col:linter:message
            # We'll compare: file:linter:message
            # Extract file (field 1), linter (field 4), and message (field 5+) from current issue
            local file_linter_msg
            file_linter_msg=$(echo "${line}" | cut -d: -f1,4,5-)

            # Check if baseline has a matching issue (same file, linter, message but possibly different line number)
            # We need to extract the same fields from baseline and compare
            local found=false
            if [[ -f "${baseline_file}" ]]; then
                while IFS= read -r baseline_line; do
                    [[ -z "${baseline_line}" ]] && continue
                    local baseline_file_linter_msg
                    baseline_file_linter_msg=$(echo "${baseline_line}" | cut -d: -f1,4,5-)
                    if [[ "${file_linter_msg}" == "${baseline_file_linter_msg}" ]]; then
                        found=true
                        break
                    fi
                done < "${baseline_file}"
            fi

            if $found; then
                echo "${line}" >> "${preexisting_issues_file}"
            else
                echo "${line}" >> "${new_issues_file}"
            fi
        done < "${current_file}"
    fi

    # Find FIXED issues - issues in baseline that are NOT in current
    if [[ -f "${baseline_file}" && -s "${baseline_file}" ]]; then
        while IFS= read -r baseline_line; do
            [[ -z "${baseline_line}" ]] && continue

            local file
            file=$(echo "${baseline_line}" | cut -d: -f1)

            # Only check files that were changed
            if [[ ${#changed_files[@]} -gt 0 ]]; then
                local file_changed=false
                for changed_file in "${changed_files[@]}"; do
                    if [[ "${file}" == "${changed_file}" ]]; then
                        file_changed=true
                        break
                    fi
                done

                # Skip if file wasn't changed
                if ! $file_changed; then
                    continue
                fi
            fi

            # Extract file:linter:message from baseline
            local baseline_file_linter_msg
            baseline_file_linter_msg=$(echo "${baseline_line}" | cut -d: -f1,4,5-)

            # Check if this issue still exists in current
            local still_exists=false
            if [[ -f "${current_file}" ]]; then
                while IFS= read -r current_line; do
                    [[ -z "${current_line}" ]] && continue
                    local current_file_linter_msg
                    current_file_linter_msg=$(echo "${current_line}" | cut -d: -f1,4,5-)
                    if [[ "${baseline_file_linter_msg}" == "${current_file_linter_msg}" ]]; then
                        still_exists=true
                        break
                    fi
                done < "${current_file}"
            fi

            # If issue doesn't exist in current, it was fixed
            if ! $still_exists; then
                echo "${baseline_line}" >> "${fixed_issues_file}"
            fi
        done < "${baseline_file}"
    fi
}

# Output issue (basic format)
output_issue() {
    echo "$1"
}

# Generate basic report
generate_report() {
    local new_issues_file="$1"
    local preexisting_issues_file="$2"
    local fixed_issues_file="$3"
    local baseline_file="$4"

    local new_count=0
    local preexisting_count=0
    local fixed_count=0
    local baseline_count=0

    [[ -f "${new_issues_file}" ]] && new_count=$(wc -l < "${new_issues_file}" | tr -d ' ')
    [[ -f "${preexisting_issues_file}" ]] && preexisting_count=$(wc -l < "${preexisting_issues_file}" | tr -d ' ')
    [[ -f "${fixed_issues_file}" ]] && fixed_count=$(wc -l < "${fixed_issues_file}" | tr -d ' ')
    [[ -f "${baseline_file}" ]] && baseline_count=$(wc -l < "${baseline_file}" | tr -d ' ')

    local current_total=$((new_count + preexisting_count))

    # Summary header
    echo "API Lint Diff Results"
    echo "====================="
    echo "Baseline (${BASELINE_BRANCH}): ${baseline_count} issues"
    echo "Current branch: ${current_total} issues"
    echo ""
    echo "FIXED: ${fixed_count}"
    echo "NEW: ${new_count}"
    echo "PRE-EXISTING: ${preexisting_count}"
    echo ""

    # Show FIXED issues
    if [[ ${fixed_count} -gt 0 ]]; then
        echo "=== FIXED ISSUES ==="
        while IFS= read -r line; do
            output_issue "${line}"
        done < "${fixed_issues_file}"
        echo ""
    fi

    # Show NEW issues
    if [[ ${new_count} -gt 0 ]]; then
        echo "=== NEW ISSUES ==="
        while IFS= read -r line; do
            output_issue "${line}"
        done < "${new_issues_file}"
        echo ""
    fi

    # Show PRE-EXISTING issues
    if [[ ${preexisting_count} -gt 0 ]]; then
        echo "=== PRE-EXISTING ISSUES ==="
        while IFS= read -r line; do
            output_issue "${line}"
        done < "${preexisting_issues_file}"
        echo ""
    fi

    # Exit based on NEW issues count
    if [[ ${new_count} -eq 0 ]]; then
        if [[ ${fixed_count} -gt 0 ]]; then
            echo -e "${GREEN}SUCCESS: Fixed ${fixed_count} issue(s), no new issues introduced.${NC}"
        else
            echo -e "${GREEN}NO NEW ISSUES found. Lint check passed.${NC}"
        fi
        if [[ ${preexisting_count} -gt 0 ]]; then
            echo -e "${YELLOW}WARNING: ${preexisting_count} pre-existing issue(s) remain. Please address them separately.${NC}"
        fi
        return 0
    else
        echo -e "${RED}FAILED: ${new_count} new issue(s) introduced${NC}"
        return 1
    fi
}

# Main execution
main() {
    # Find golangci-lint
    BASE_LINTER_PATH=$(find_golangci_lint)

    # Build custom linter with kube-api-linter plugin
    LINTER_PATH="${BASE_LINTER_PATH}"
    if CUSTOM_LINTER=$(build_custom_linter "${BASE_LINTER_PATH}"); then
        LINTER_PATH="${CUSTOM_LINTER}"
    fi

    # Convert to absolute path if needed
    if [[ "${LINTER_PATH}" != /* ]]; then
        LINTER_PATH="$(pwd)/${LINTER_PATH}"
    fi

    # Create temporary config
    create_temp_config

    # Ensure baseline branch is available (important for CI environments like GitHub Actions)
    if ! git rev-parse --verify "${BASELINE_BRANCH}" &> /dev/null; then
        echo -e "${YELLOW}Baseline branch '${BASELINE_BRANCH}' not found locally. Fetching from origin...${NC}" >&2

        # Fetch the baseline branch from origin
        if ! git fetch origin "${BASELINE_BRANCH}:${BASELINE_BRANCH}" 2>&1; then
            # If direct fetch fails, try fetching with remote tracking
            if ! git fetch origin "${BASELINE_BRANCH}" 2>&1; then
                echo -e "${RED}Error: Failed to fetch baseline branch '${BASELINE_BRANCH}' from origin${NC}" >&2
                echo -e "${RED}Please ensure the branch exists in the remote repository.${NC}" >&2
                exit 1
            fi
            # Use the remote tracking branch, adding 'origin/' only if not already present
            if [[ "${BASELINE_BRANCH}" != origin/* ]]; then
                BASELINE_BRANCH="origin/${BASELINE_BRANCH}"
            fi
        fi
    fi

    # Get changed files
    get_changed_files > "${TEMP_DIR}/changed_files.txt"

    # Run linter on current branch
    REPO_ROOT="$(pwd)"
    run_linter "${TEMP_DIR}/.golangci.yaml" "${TEMP_DIR}/current_output.txt" "${LINTER_PATH}" "${REPO_ROOT}"
    parse_linter_output "${TEMP_DIR}/current_output.txt" "${TEMP_DIR}/current_parsed.txt"

    # Run linter on baseline
    WORKTREE_DIR="${TEMP_DIR}/baseline_worktree"
    if ! git worktree add --detach "${WORKTREE_DIR}" "${BASELINE_BRANCH}" 2>&1; then
        echo -e "${RED}Error: Failed to create git worktree for baseline branch '${BASELINE_BRANCH}'${NC}" >&2
        echo -e "${RED}Please ensure the branch exists and try again.${NC}" >&2
        exit 1
    fi
    run_linter "${TEMP_DIR}/.golangci.yaml" "${TEMP_DIR}/baseline_output.txt" "${LINTER_PATH}" "${WORKTREE_DIR}"
    parse_linter_output "${TEMP_DIR}/baseline_output.txt" "${TEMP_DIR}/baseline_parsed.txt"
    # Worktree cleanup is handled by the cleanup trap

    # Categorize issues
    touch "${TEMP_DIR}/new_issues.txt"
    touch "${TEMP_DIR}/preexisting_issues.txt"
    touch "${TEMP_DIR}/fixed_issues.txt"

    categorize_issues \
        "${TEMP_DIR}/current_parsed.txt" \
        "${TEMP_DIR}/baseline_parsed.txt" \
        "${TEMP_DIR}/changed_files.txt" \
        "${TEMP_DIR}/new_issues.txt" \
        "${TEMP_DIR}/preexisting_issues.txt" \
        "${TEMP_DIR}/fixed_issues.txt"

    # Generate report
    generate_report \
        "${TEMP_DIR}/new_issues.txt" \
        "${TEMP_DIR}/preexisting_issues.txt" \
        "${TEMP_DIR}/fixed_issues.txt" \
        "${TEMP_DIR}/baseline_parsed.txt"

    return $?
}

# Run main function
main "$@"
