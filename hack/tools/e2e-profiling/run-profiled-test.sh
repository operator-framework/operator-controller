#!/bin/bash
#
# Run e2e test with memory profiling
#
# Usage: run-profiled-test.sh <test-name> [test-target]
#
# test-target options:
#   - test-e2e (standard e2e)
#   - test-experimental-e2e (experimental e2e) [default]
#   - test-extension-developer-e2e (extension developer e2e)
#   - test-upgrade-e2e (upgrade e2e)
#   - test-upgrade-experimental-e2e (upgrade experimental e2e)
#

set -euo pipefail

# Configuration
TEST_NAME="${1:-default}"
TEST_TARGET="${2:-${E2E_PROFILE_TEST_TARGET:-test-experimental-e2e}}"
OUTPUT_DIR="${E2E_PROFILE_DIR:-./e2e-profiles}/${TEST_NAME}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Get absolute path for OUTPUT_DIR
# This is needed because E2E tests may change directories
OUTPUT_DIR_ABS="$(cd "$(dirname "${OUTPUT_DIR}")" 2>/dev/null && pwd)/$(basename "${OUTPUT_DIR}")" || OUTPUT_DIR_ABS="${OUTPUT_DIR}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

# Create output directory (clean up if it exists)
if [ -d "${OUTPUT_DIR}" ]; then
    log_warn "Output directory already exists: ${OUTPUT_DIR}"
    log_warn "Removing old profiles to start fresh..."
    rm -rf "${OUTPUT_DIR}"
fi
mkdir -p "${OUTPUT_DIR}"

# Update OUTPUT_DIR_ABS now that directory exists
OUTPUT_DIR_ABS="$(cd "${OUTPUT_DIR}" && pwd)"

# PIDs to track
TEST_PID=""
COLLECT_PID=""

# Cleanup function
cleanup() {
    log_info "Cleaning up..."

    if [ -n "${COLLECT_PID}" ] && kill -0 "${COLLECT_PID}" 2>/dev/null; then
        log_info "Stopping profile collection (PID: ${COLLECT_PID})"
        kill "${COLLECT_PID}" 2>/dev/null || true
        wait "${COLLECT_PID}" 2>/dev/null || true
    fi

    if [ -n "${TEST_PID}" ] && kill -0 "${TEST_PID}" 2>/dev/null; then
        log_warn "Test is still running (PID: ${TEST_PID})"
        log_warn "Leaving test running. Use 'kill ${TEST_PID}' to stop it."
    fi
}

trap cleanup EXIT INT TERM

# Check if make target exists
if ! make -n "${TEST_TARGET}" >/dev/null 2>&1; then
    log_error "Make target '${TEST_TARGET}' not found"
    log_error "Ensure you're in the project root directory"
    log_error "Available e2e targets: test-e2e, test-experimental-e2e, test-extension-developer-e2e, test-upgrade-e2e, test-upgrade-experimental-e2e"
    exit 1
fi

log_info "Starting profiled test: ${TEST_NAME}"
log_info "Test target: ${TEST_TARGET}"
log_info "Output directory: ${OUTPUT_DIR}"

# Start the e2e test
log_info "Starting e2e test (${TEST_TARGET})..."
# Set E2E_SUMMARY_OUTPUT to capture prometheus alerts and other test metrics
# Use absolute path because e2e tests may change directories
E2E_SUMMARY_OUTPUT="${OUTPUT_DIR_ABS}/e2e-summary.md" make "${TEST_TARGET}" > "${OUTPUT_DIR}/test.log" 2>&1 &
TEST_PID=$!
log_info "Test started (PID: ${TEST_PID})"

# Give the test some time to start
log_info "Waiting for test to initialize (30 seconds)..."
sleep 30

# Check if test is still running
if ! kill -0 "${TEST_PID}" 2>/dev/null; then
    # Capture the exit code of the test process
    wait "${TEST_PID}"
    TEST_EXIT_CODE=$?
    log_error "Test exited early with exit code ${TEST_EXIT_CODE}!"
    log_error "Check ${OUTPUT_DIR}/test.log for details"
    exit "${TEST_EXIT_CODE}"
fi

# Start profile collection
log_info "Starting profile collection..."
"${SCRIPT_DIR}/collect-profiles.sh" "${TEST_NAME}" > "${OUTPUT_DIR}/collection.log" 2>&1 &
COLLECT_PID=$!
log_info "Profile collection started (PID: ${COLLECT_PID})"

# Monitor both processes
log_info "Monitoring test and collection..."
log_info "Test PID: ${TEST_PID}"
log_info "Collection PID: ${COLLECT_PID}"
log_info ""
log_info "Press Ctrl+C to stop collection (test will continue)"
log_info ""

# Wait for either process to finish
while true; do
    # Check if test finished
    if ! kill -0 "${TEST_PID}" 2>/dev/null; then
        log_info "Test completed"
        TEST_EXIT=$?

        # Give collection a few more seconds
        log_info "Collecting final profiles..."
        sleep 30

        # Stop collection
        if [ -n "${COLLECT_PID}" ] && kill -0 "${COLLECT_PID}" 2>/dev/null; then
            kill "${COLLECT_PID}" 2>/dev/null || true
        fi

        break
    fi

    # Check if collection stopped
    if ! kill -0 "${COLLECT_PID}" 2>/dev/null; then
        log_warn "Profile collection stopped"
        log_info "Test is still running (PID: ${TEST_PID})"
        log_info "Waiting for test to complete..."

        # Wait for test to finish
        wait "${TEST_PID}" 2>/dev/null || true
        TEST_EXIT=$?
        break
    fi

    # Display progress
    if [ -d "${OUTPUT_DIR}/operator-controller" ] && [ -d "${OUTPUT_DIR}/catalogd" ]; then
        # Multi-component progress
        OC_COUNT=$(find -L "${OUTPUT_DIR}/operator-controller" -maxdepth 1 -name "heap*.pprof" -type f 2>/dev/null | wc -l)
        CAT_COUNT=$(find -L "${OUTPUT_DIR}/catalogd" -maxdepth 1 -name "heap*.pprof" -type f 2>/dev/null | wc -l)
        echo -ne "\r${BLUE}[PROGRESS]${NC} operator-controller: ${OC_COUNT}, catalogd: ${CAT_COUNT}  "
    else
        # Single-component progress
        PROFILE_COUNT=$(find -L "${OUTPUT_DIR}" -maxdepth 1 -name "heap*.pprof" -type f 2>/dev/null | wc -l)
        if [ "${PROFILE_COUNT}" -gt 0 ]; then
            LATEST=$(ls -t "${OUTPUT_DIR}"/heap*.pprof 2>/dev/null | head -1)
            LATEST_SIZE=$(du -h "${LATEST}" 2>/dev/null | cut -f1 || echo "?")
            echo -ne "\r${BLUE}[PROGRESS]${NC} Profiles: ${PROFILE_COUNT}, Latest: $(basename "${LATEST}") (${LATEST_SIZE})  "
        fi
    fi

    sleep 5
done

echo "" # New line after progress

# Count collected profiles
if [ -d "${OUTPUT_DIR}/operator-controller" ] && [ -d "${OUTPUT_DIR}/catalogd" ]; then
    OC_FINAL=$(find -L "${OUTPUT_DIR}/operator-controller" -maxdepth 1 -name "heap*.pprof" -type f 2>/dev/null | wc -l)
    CAT_FINAL=$(find -L "${OUTPUT_DIR}/catalogd" -maxdepth 1 -name "heap*.pprof" -type f 2>/dev/null | wc -l)
    FINAL_COUNT=$((OC_FINAL + CAT_FINAL))
    log_success "Profiling complete!"
    log_info "Collected ${OC_FINAL} operator-controller profiles and ${CAT_FINAL} catalogd profiles"
    log_info "Profiles saved to: ${OUTPUT_DIR}"
else
    FINAL_COUNT=$(find -L "${OUTPUT_DIR}" -maxdepth 1 -name "heap*.pprof" -type f 2>/dev/null | wc -l)
    log_success "Profiling complete!"
    log_info "Collected ${FINAL_COUNT} profiles"
    log_info "Profiles saved to: ${OUTPUT_DIR}"
fi

# Run analysis
log_info "Running analysis..."
if "${SCRIPT_DIR}/analyze-profiles.sh" "${TEST_NAME}"; then
    log_success "Analysis complete!"
else
    log_error "Analysis failed"
fi

# Display summary
echo ""
echo "=== Test Summary ==="
echo "Test: ${TEST_NAME}"
if [ -d "${OUTPUT_DIR}/operator-controller" ] && [ -d "${OUTPUT_DIR}/catalogd" ]; then
    echo "operator-controller: ${OC_FINAL} profiles"
    echo "catalogd: ${CAT_FINAL} profiles"
else
    echo "Profiles: ${FINAL_COUNT}"
fi
echo "Output: ${OUTPUT_DIR}"
echo "Test Log: ${OUTPUT_DIR}/test.log"
echo "Collection Log: ${OUTPUT_DIR}/collection.log"
echo "Analysis: ${OUTPUT_DIR}/analysis.md"
echo ""

if [ "${FINAL_COUNT}" -eq 0 ]; then
    log_error "No profiles collected!"
    log_error "Check collection.log for errors"
    exit 1
fi

log_success "All done! Review the analysis in ${OUTPUT_DIR}/analysis.md"
