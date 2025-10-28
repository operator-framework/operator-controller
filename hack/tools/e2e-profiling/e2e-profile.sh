#!/bin/bash
#
# E2E profiling wrapper script
# Main entry point for the e2e profiling plugin
#
# Usage:
#   ./e2e-profile.sh run <test-name>
#   ./e2e-profile.sh analyze <test-name>
#   ./e2e-profile.sh compare <test1> <test2>
#   ./e2e-profile.sh collect
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Colors
BLUE='\033[0;34m'
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

usage() {
    cat << EOF
E2E Profiling Tool

USAGE:
    $0 <command> [arguments]

COMMANDS:
    run <test-name> [test-target]   Run e2e test with heap and CPU profiling
    analyze <test-name>             Analyze collected profiles
    compare <test1> <test2>         Compare two test runs
    collect                         Manually collect a single profile
    help                            Show this help message

TEST TARGETS (for 'run' command):
    test-e2e                        Standard e2e tests
    test-experimental-e2e           Experimental e2e tests (default)
    test-extension-developer-e2e    Extension developer e2e tests
    test-upgrade-e2e                Upgrade e2e tests
    test-upgrade-experimental-e2e   Upgrade experimental e2e tests

EXAMPLES:
    $0 run baseline
    $0 run baseline test-e2e
    $0 run with-caching test-experimental-e2e
    $0 compare baseline with-caching
    $0 analyze baseline

ENVIRONMENT VARIABLES:
    E2E_PROFILE_NAMESPACE       Namespace (default: olmv1-system)
    E2E_PROFILE_INTERVAL        Collection interval in seconds (default: 15)
    E2E_PROFILE_CPU_DURATION    CPU sampling duration in seconds (default: 10)
    E2E_PROFILE_DIR             Output directory (default: ./e2e-profiles)
    E2E_PROFILE_TEST_TARGET     Default test target (default: test-experimental-e2e)

EOF
}

log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $*"
}

# Parse command
COMMAND="${1:-}"

case "${COMMAND}" in
    run)
        TEST_NAME="${2:-}"
        TEST_TARGET="${3:-}"
        if [ -z "${TEST_NAME}" ]; then
            log_error "Test name required"
            echo "Usage: $0 run <test-name> [test-target]"
            exit 1
        fi
        exec "${SCRIPT_DIR}/run-profiled-test.sh" "${TEST_NAME}" "${TEST_TARGET}"
        ;;

    analyze)
        TEST_NAME="${2:-}"
        if [ -z "${TEST_NAME}" ]; then
            log_error "Test name required"
            echo "Usage: $0 analyze <test-name>"
            exit 1
        fi
        exec "${SCRIPT_DIR}/analyze-profiles.sh" "${TEST_NAME}"
        ;;

    compare)
        TEST1="${2:-}"
        TEST2="${3:-}"
        if [ -z "${TEST1}" ] || [ -z "${TEST2}" ]; then
            log_error "Two test names required"
            echo "Usage: $0 compare <test1> <test2>"
            exit 1
        fi
        exec "${SCRIPT_DIR}/compare-profiles.sh" "${TEST1}" "${TEST2}"
        ;;

    collect)
        exec "${SCRIPT_DIR}/collect-profiles.sh" "manual"
        ;;

    help|--help|-h)
        usage
        exit 0
        ;;

    "")
        log_error "No command specified"
        usage
        exit 1
        ;;

    *)
        log_error "Unknown command: ${COMMAND}"
        usage
        exit 1
        ;;
esac
