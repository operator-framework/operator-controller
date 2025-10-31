#!/bin/bash
#
# Collect heap profiles from operator-controller and catalogd during e2e test
#
# Usage: collect-profiles.sh <test-name>
#

set -euo pipefail

# Configuration
TEST_NAME="${1:-default}"
NAMESPACE="${E2E_PROFILE_NAMESPACE:-olmv1-system}"
INTERVAL="${E2E_PROFILE_INTERVAL:-10}"
OUTPUT_DIR="${E2E_PROFILE_DIR:-./e2e-profiles}/${TEST_NAME}"
CPU_PROFILE_DURATION="${E2E_PROFILE_CPU_DURATION:-10}"  # CPU sampling duration in seconds
PROFILE_MODE="${E2E_PROFILE_MODE:-both}"  # Options: both, heap, cpu

# Component configurations
declare -A COMPONENTS=(
    ["operator-controller"]="deployment=operator-controller-controller-manager;label=app.kubernetes.io/name=operator-controller;port=6060;local_port=6060"
    ["catalogd"]="deployment=catalogd-controller-manager;label=app.kubernetes.io/name=catalogd;port=6060;local_port=6061"
)

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*"
}

# Validate profile mode
case "${PROFILE_MODE}" in
    both|heap|cpu)
        ;;
    *)
        log_error "Invalid E2E_PROFILE_MODE: ${PROFILE_MODE}"
        log_error "Valid options: both, heap, cpu"
        exit 1
        ;;
esac

# Create output directory
mkdir -p "${OUTPUT_DIR}"
log_info "Output directory: ${OUTPUT_DIR}"
log_info "Profile mode: ${PROFILE_MODE}"

# PIDs to track
declare -A PF_PIDS
declare -A PODS

# Cleanup function
cleanup() {
    log_info "Cleaning up..."
    for component in "${!PF_PIDS[@]}"; do
        if [ -n "${PF_PIDS[$component]:-}" ]; then
            log_info "Killing port-forward for ${component} (PID: ${PF_PIDS[$component]})"
            kill "${PF_PIDS[$component]}" 2>/dev/null || true
        fi
    done
}

trap cleanup EXIT INT TERM

# Wait for namespace to exist first
log_info "Waiting for namespace ${NAMESPACE} to exist..."
TIMEOUT=300  # 5 minutes
ELAPSED=0
NAMESPACE_CHECK_INTERVAL=5
while [ $ELAPSED -lt $TIMEOUT ]; do
    if kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1; then
        log_success "Namespace ${NAMESPACE} exists"
        break
    fi
    sleep $NAMESPACE_CHECK_INTERVAL
    ELAPSED=$((ELAPSED + NAMESPACE_CHECK_INTERVAL))
done

if [ $ELAPSED -ge $TIMEOUT ]; then
    log_error "Namespace ${NAMESPACE} did not appear within ${TIMEOUT} seconds"
    exit 1
fi

# Set up port forwarding for all components
for component in "${!COMPONENTS[@]}"; do
    # Parse component configuration
    IFS=';' read -ra CONFIG <<< "${COMPONENTS[$component]}"
    DEPLOYMENT=""
    POD_LABEL=""
    PPROF_PORT=""
    LOCAL_PORT=""

    for item in "${CONFIG[@]}"; do
        key="${item%%=*}"
        value="${item#*=}"
        case "$key" in
            deployment) DEPLOYMENT="$value" ;;
            label) POD_LABEL="$value" ;;
            port) PPROF_PORT="$value" ;;
            local_port) LOCAL_PORT="$value" ;;
        esac
    done

    log_info "Setting up ${component}..."

    # Wait for deployment to exist first
    log_info "Waiting for deployment ${DEPLOYMENT} to be created in namespace ${NAMESPACE}..."
    TIMEOUT=300
    ELAPSED=0
    while ! kubectl get deployment -n "${NAMESPACE}" "${DEPLOYMENT}" &> /dev/null; do
        if [ $ELAPSED -ge $TIMEOUT ]; then
            log_error "Deployment ${DEPLOYMENT} was not created within ${TIMEOUT} seconds"
            exit 1
        fi
        sleep 2
        ELAPSED=$((ELAPSED + 2))
    done
    log_success "Deployment ${DEPLOYMENT} exists"

    # Now wait for deployment to become available
    log_info "Waiting for deployment ${DEPLOYMENT} to become available..."
    if ! kubectl wait --for=condition=Available -n "${NAMESPACE}" deployment "${DEPLOYMENT}" --timeout=300s; then
        log_error "Deployment ${DEPLOYMENT} did not become available in time"
        exit 1
    fi
    log_success "Deployment ${DEPLOYMENT} is available"

    # Get pod name
    log_info "Finding pod with label: ${POD_LABEL}"
    POD=$(kubectl get pod -n "${NAMESPACE}" -l "${POD_LABEL}" -o name | head -1)
    if [ -z "${POD}" ]; then
        log_error "No pod found with label ${POD_LABEL}"
        exit 1
    fi
    log_success "Found pod: ${POD}"
    PODS[$component]="${POD}"

    # Create component output directory
    mkdir -p "${OUTPUT_DIR}/${component}"

    # Set up port forwarding
    log_info "Setting up port forwarding to ${POD}:${PPROF_PORT} -> localhost:${LOCAL_PORT}..."
    kubectl port-forward -n "${NAMESPACE}" "${POD}" "${LOCAL_PORT}:${PPROF_PORT}" > "${OUTPUT_DIR}/${component}/port-forward.log" 2>&1 &
    PF_PIDS[$component]=$!
    log_info "Port-forward started (PID: ${PF_PIDS[$component]})"
done

# Wait for port forwards to be ready
log_info "Waiting for port forwards to initialize..."
sleep 5

# Test connections
for component in "${!COMPONENTS[@]}"; do
    IFS=';' read -ra CONFIG <<< "${COMPONENTS[$component]}"
    LOCAL_PORT=""
    for item in "${CONFIG[@]}"; do
        key="${item%%=*}"
        value="${item#*=}"
        if [ "$key" = "local_port" ]; then
            LOCAL_PORT="$value"
            break
        fi
    done

    if ! curl -s --max-time 5 "http://localhost:${LOCAL_PORT}/debug/pprof/" > /dev/null; then
        log_error "Failed to connect to ${component} pprof endpoint"
        log_error "Check ${OUTPUT_DIR}/${component}/port-forward.log for details"
        exit 1
    fi
    log_success "Connected to ${component} pprof endpoint"
done

# Collect profiles from all components simultaneously
n=0
log_info "Starting profile collection (interval: ${INTERVAL}s, CPU sampling: ${CPU_PROFILE_DURATION}s)"
log_info "Press Ctrl+C to stop"

while true; do
    # Record iteration start time
    iteration_start=$(date +%s)

    all_success=true
    cpu_pids=()

    for component in "${!COMPONENTS[@]}"; do
        IFS=';' read -ra CONFIG <<< "${COMPONENTS[$component]}"
        LOCAL_PORT=""
        for item in "${CONFIG[@]}"; do
            key="${item%%=*}"
            value="${item#*=}"
            if [ "$key" = "local_port" ]; then
                LOCAL_PORT="$value"
                break
            fi
        done

        # Collect heap profile (if enabled)
        if [ "${PROFILE_MODE}" = "both" ] || [ "${PROFILE_MODE}" = "heap" ]; then
            HEAP_FILE="${OUTPUT_DIR}/${component}/heap${n}.pprof"
            if curl -s --max-time 10 "http://localhost:${LOCAL_PORT}/debug/pprof/heap" > "${HEAP_FILE}"; then
                SIZE=$(du -h "${HEAP_FILE}" | cut -f1)
                log_success "Collected ${component}/heap${n}.pprof (${SIZE})"

                # Quick check if file is valid
                if [ ! -s "${HEAP_FILE}" ]; then
                    log_warn "${component} heap profile file is empty"
                    rm "${HEAP_FILE}"
                    all_success=false
                fi
            else
                log_error "Failed to collect ${component} heap profile"
                all_success=false
            fi
        fi

        # Collect CPU profile (in background to avoid blocking, if enabled)
        if [ "${PROFILE_MODE}" = "both" ] || [ "${PROFILE_MODE}" = "cpu" ]; then
            CPU_FILE="${OUTPUT_DIR}/${component}/cpu${n}.pprof"
            (
                if curl -s --max-time $((CPU_PROFILE_DURATION + 5)) "http://localhost:${LOCAL_PORT}/debug/pprof/profile?seconds=${CPU_PROFILE_DURATION}" > "${CPU_FILE}"; then
                    if [ -s "${CPU_FILE}" ]; then
                        SIZE=$(du -h "${CPU_FILE}" | cut -f1)
                        log_success "Collected ${component}/cpu${n}.pprof (${SIZE})"
                    else
                        log_warn "${component} CPU profile file is empty"
                        rm "${CPU_FILE}" 2>/dev/null || true
                    fi
                else
                    log_error "Failed to collect ${component} CPU profile"
                    rm "${CPU_FILE}" 2>/dev/null || true
                fi
            ) &
            cpu_pids+=($!)
        fi
    done

    if [ "$all_success" = false ]; then
        log_warn "One or more heap profiles failed to collect, stopping"
        # Wait for any background CPU profiling to complete
        for pid in "${cpu_pids[@]}"; do
            wait "$pid" 2>/dev/null || true
        done
        break
    fi

    n=$((n + 1))

    # Wait for CPU profiling to complete before next iteration
    for pid in "${cpu_pids[@]}"; do
        wait "$pid" 2>/dev/null || true
    done

    # Calculate sleep time to maintain consistent interval
    # Account for time already spent in heap collection and CPU profiling
    iteration_end=$(date +%s)
    elapsed=$((iteration_end - iteration_start))
    sleep_time=$((INTERVAL - elapsed))

    if [ $sleep_time -gt 0 ]; then
        sleep "${sleep_time}"
    else
        log_warn "Iteration took ${elapsed}s, longer than interval ${INTERVAL}s. Skipping sleep."
    fi
done

log_success "Collection complete. Collected ${n} profiles from each component."

# Clean up empty profiles (created during cluster teardown)
log_info "Cleaning up empty profile files..."
for component_name in "${!COMPONENTS[@]}"; do
    component_dir="${OUTPUT_DIR}/${component_name}"
    if [ -d "${component_dir}" ]; then
        # Clean up empty heap profiles
        empty_heap_count=$(find "${component_dir}" -name "heap*.pprof" -type f -size 0 2>/dev/null | wc -l)
        if [ "${empty_heap_count}" -gt 0 ]; then
            log_info "  Removing ${empty_heap_count} empty heap profiles from ${component_name}"
            find "${component_dir}" -name "heap*.pprof" -type f -size 0 -delete
        fi

        # Clean up empty CPU profiles
        empty_cpu_count=$(find "${component_dir}" -name "cpu*.pprof" -type f -size 0 2>/dev/null | wc -l)
        if [ "${empty_cpu_count}" -gt 0 ]; then
            log_info "  Removing ${empty_cpu_count} empty CPU profiles from ${component_name}"
            find "${component_dir}" -name "cpu*.pprof" -type f -size 0 -delete
        fi
    fi
done

log_info "Profiles saved to: ${OUTPUT_DIR}"
