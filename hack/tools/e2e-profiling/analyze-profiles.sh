#!/bin/bash
#
# Analyze collected heap profiles and generate report
# Supports both single-component and multi-component analysis
#
# Usage: analyze-profiles.sh <test-name>
#

set -euo pipefail

# Configuration
TEST_NAME="${1:-default}"
OUTPUT_DIR="${E2E_PROFILE_DIR:-./e2e-profiles}/${TEST_NAME}"
# Convert to absolute path to avoid issues with cd
OUTPUT_DIR="$(cd "${OUTPUT_DIR}" 2>/dev/null && pwd || echo "${OUTPUT_DIR}")"
REPORT_FILE="${OUTPUT_DIR}/analysis.md"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
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

# Function to analyze a single component's profiles
# Arguments: component_name component_dir
# Returns: 0 on success, 1 on error
# Appends analysis sections to REPORT_FILE
# Outputs peak memory total to stdout (for capture)
analyze_component() {
    local component_name="$1"
    local component_dir="$2"

    log_info "Analyzing ${component_name}..." >&2

    # Check if profiles exist for this component
    local profile_count=$(find -L "${component_dir}" -maxdepth 1 -name "heap*.pprof" -type f 2>/dev/null | wc -l)
    if [ "${profile_count}" -eq 0 ]; then
        log_error "No heap profiles found for ${component_name} in ${component_dir}" >&2
        return 1
    fi

    log_info "  Found ${profile_count} profiles for ${component_name}" >&2

    # Find the largest profile (peak memory)
    local peak_profile=$(ls -lS "${component_dir}"/heap*.pprof 2>/dev/null | head -1 | awk '{print $NF}')
    local peak_name=$(basename "${peak_profile}")
    local peak_size=$(du -h "${peak_profile}" | cut -f1)

    log_info "  Peak profile: ${peak_name} (${peak_size})" >&2

    # Get baseline profile
    local baseline_profile="${component_dir}/heap0.pprof"

    # Extract peak memory stats
    log_info "  Extracting peak memory statistics..." >&2
    local peak_stats=$(cd "${component_dir}" && go tool pprof -top "${peak_name}" 2>/dev/null | head -6)
    local peak_total=$(echo "${peak_stats}" | grep "^Showing" | awk '{print $7, $8}')

    # Write component header
    cat >> "${REPORT_FILE}" << EOF

## ${component_name} Analysis

**Profiles Collected:** ${profile_count}
**Peak Profile:** ${peak_name} (${peak_size})
**Peak Memory Usage:** ${peak_total}

### Memory Growth

| Snapshot | File Size | Growth from Previous |
|----------|-----------|---------------------|
EOF

    # Add file sizes with growth
    local prev_size=0
    for f in $(ls "${component_dir}"/heap*.pprof 2>/dev/null | sort -V); do
        local name=$(basename "$f")
        local size=$(stat -c%s "$f")
        local size_kb=$((size / 1024))

        if [ $prev_size -eq 0 ]; then
            local growth="baseline"
        else
            local growth_kb=$((size_kb - prev_size))
            if [ $growth_kb -gt 0 ]; then
                growth="+${growth_kb}K"
            elif [ $growth_kb -lt 0 ]; then
                growth="${growth_kb}K"
            else
                growth="0"
            fi
        fi

        echo "| ${name} | ${size_kb}K | ${growth} |" >> "${REPORT_FILE}"
        prev_size=$size_kb
    done

    # Top allocators from peak
    log_info "  Extracting top allocators..." >&2
    cat >> "${REPORT_FILE}" << 'EOF'

### Top Memory Allocators (Peak Profile)

```
EOF

    cd "${component_dir}" && go tool pprof -top "${peak_name}" 2>/dev/null | head -20 >> "${REPORT_FILE}"

    cat >> "${REPORT_FILE}" << 'EOF'
```

EOF

    # OpenAPI-specific analysis
    log_info "  Analyzing OpenAPI allocations..." >&2
    cat >> "${REPORT_FILE}" << 'EOF'

### OpenAPI-Related Allocations

```
EOF

    cd "${component_dir}" && go tool pprof -text "${peak_name}" 2>/dev/null | grep -i openapi | head -20 >> "${REPORT_FILE}" || echo "No OpenAPI allocations found" >> "${REPORT_FILE}"

    cat >> "${REPORT_FILE}" << 'EOF'
```

EOF

    # Growth analysis (baseline to peak)
    if [ -f "${baseline_profile}" ]; then
        log_info "  Analyzing growth from baseline to peak..." >&2
        cat >> "${REPORT_FILE}" << 'EOF'

### Memory Growth Analysis (Baseline to Peak)

#### Top Growth Contributors

```
EOF

        cd "${component_dir}" && go tool pprof -base="${baseline_profile}" -top "${peak_profile}" 2>/dev/null | head -20 >> "${REPORT_FILE}"

        cat >> "${REPORT_FILE}" << 'EOF'
```

#### OpenAPI Growth

```
EOF

        cd "${component_dir}" && go tool pprof -base="${baseline_profile}" -text "${peak_profile}" 2>/dev/null | grep -i openapi | head -20 >> "${REPORT_FILE}" || echo "No OpenAPI growth detected" >> "${REPORT_FILE}"

        cat >> "${REPORT_FILE}" << 'EOF'
```

#### JSON Deserialization Growth

```
EOF

        cd "${component_dir}" && go tool pprof -base="${baseline_profile}" -text "${peak_profile}" 2>/dev/null | grep -iE "(json|unmarshal|decode)" | head -20 >> "${REPORT_FILE}" || echo "No JSON deserialization growth detected" >> "${REPORT_FILE}"

        cat >> "${REPORT_FILE}" << 'EOF'
```

#### Dynamic Client Growth

```
EOF

        cd "${component_dir}" && go tool pprof -base="${baseline_profile}" -text "${peak_profile}" 2>/dev/null | grep -iE "(dynamic|List|Informer)" | head -20 >> "${REPORT_FILE}" || echo "No dynamic client growth detected" >> "${REPORT_FILE}"

        cat >> "${REPORT_FILE}" << 'EOF'
```
EOF
    fi

    # Return peak memory for summary
    echo "${peak_total}"
}

# Function to analyze a single component's CPU profiles
# Arguments: component_name component_dir
# Returns: 0 on success, 1 on skip (no profiles), 2 on error
# Appends analysis sections to REPORT_FILE
analyze_cpu_component() {
    local component_name="$1"
    local component_dir="$2"

    log_info "Analyzing ${component_name} CPU profiles..." >&2

    # Check if CPU profiles exist for this component
    local cpu_profile_count=$(find -L "${component_dir}" -maxdepth 1 -name "cpu*.pprof" -type f 2>/dev/null | wc -l)
    if [ "${cpu_profile_count}" -eq 0 ]; then
        log_info "  No CPU profiles found for ${component_name} (skipping CPU analysis)" >&2
        return 1
    fi

    log_info "  Found ${cpu_profile_count} CPU profiles for ${component_name}" >&2

    # Find the largest CPU profile (typically represents most activity)
    local peak_cpu_profile=$(ls -lS "${component_dir}"/cpu*.pprof 2>/dev/null | head -1 | awk '{print $NF}')
    local peak_cpu_name=$(basename "${peak_cpu_profile}")
    local peak_cpu_size=$(du -h "${peak_cpu_profile}" | cut -f1)

    log_info "  Peak CPU profile: ${peak_cpu_name} (${peak_cpu_size})" >&2

    # Extract CPU stats
    log_info "  Extracting CPU profile statistics..." >&2
    local cpu_stats=$(cd "${component_dir}" && go tool pprof -top "${peak_cpu_name}" 2>/dev/null | head -6)
    local cpu_total=$(echo "${cpu_stats}" | grep "^Showing" | awk '{print $7, $8}')

    # Write CPU analysis header
    cat >> "${REPORT_FILE}" << EOF

### CPU Profile Analysis

**CPU Profiles Collected:** ${cpu_profile_count}
**Peak CPU Profile:** ${peak_cpu_name} (${peak_cpu_size})
**Total CPU Time:** ${cpu_total}

#### Top CPU Consumers (Peak Profile)

\`\`\`
EOF

    cd "${component_dir}" && go tool pprof -top "${peak_cpu_name}" 2>/dev/null | head -20 >> "${REPORT_FILE}"

    cat >> "${REPORT_FILE}" << 'EOF'
```

#### CPU-Intensive Functions

\`\`\`
EOF

    # Look for controller reconciliation and other hot paths
    cd "${component_dir}" && go tool pprof -text "${peak_cpu_name}" 2>/dev/null | grep -iE "(Reconcile|sync|watch|cache|list)" | head -20 >> "${REPORT_FILE}" || echo "No reconciliation functions found in top CPU consumers" >> "${REPORT_FILE}"

    cat >> "${REPORT_FILE}" << 'EOF'
```

#### JSON/Serialization CPU Usage

\`\`\`
EOF

    cd "${component_dir}" && go tool pprof -text "${peak_cpu_name}" 2>/dev/null | grep -iE "(json|unmarshal|decode|marshal|encode)" | head -15 >> "${REPORT_FILE}" || echo "No significant JSON/serialization CPU usage detected" >> "${REPORT_FILE}"

    cat >> "${REPORT_FILE}" << 'EOF'
```
EOF

    return 0
}

# Check if output directory exists
if [ ! -d "${OUTPUT_DIR}" ]; then
    log_error "Directory not found: ${OUTPUT_DIR}"
    exit 1
fi

# Check for required component directories
if [ ! -d "${OUTPUT_DIR}/operator-controller" ] || [ ! -d "${OUTPUT_DIR}/catalogd" ]; then
    log_error "Expected component directories not found!"
    log_error "Directory must contain: operator-controller/ and catalogd/ subdirectories"
    log_error "Found in ${OUTPUT_DIR}:"
    ls -la "${OUTPUT_DIR}" >&2
    exit 1
fi

COMPONENTS=("operator-controller" "catalogd")

# Generate report header
log_info "Generating analysis report..."

cat > "${REPORT_FILE}" << EOF
# Memory Profile Analysis

**Test Name:** ${TEST_NAME}
**Date:** $(date '+%Y-%m-%d %H:%M:%S')

---

## Executive Summary

EOF

declare -A PEAK_MEMORY

# Analyze each component
for component in "${COMPONENTS[@]}"; do
    component_dir="${OUTPUT_DIR}/${component}"
    peak_mem=$(analyze_component "${component}" "${component_dir}")
    PEAK_MEMORY[$component]="${peak_mem}"

    # Analyze CPU profiles if available
    analyze_cpu_component "${component}" "${component_dir}" || true

    echo "" >> "${REPORT_FILE}"
    echo "---" >> "${REPORT_FILE}"
done

# Insert executive summary after the header
# Create a temporary file with the summary
TEMP_SUMMARY=$(mktemp)
for component in "${COMPONENTS[@]}"; do
    echo "- **${component}**: ${PEAK_MEMORY[$component]}" >> "${TEMP_SUMMARY}"
done
echo "" >> "${TEMP_SUMMARY}"

# Insert the summary after "## Executive Summary" line
awk '/^## Executive Summary/ {print; system("cat '"${TEMP_SUMMARY}"'"); next} 1' "${REPORT_FILE}" > "${REPORT_FILE}.tmp"
mv "${REPORT_FILE}.tmp" "${REPORT_FILE}"
rm "${TEMP_SUMMARY}"

# Prometheus Alerts Analysis (applies to entire test run, not per-component)
if [ -f "${OUTPUT_DIR}/e2e-summary.md" ]; then
    log_info "Analyzing Prometheus alerts..."
    cat >> "${REPORT_FILE}" << 'EOF'

---

## Prometheus Alerts

EOF

    # Extract the Alerts section from the markdown file
    # Look for "## Alerts" section and extract until next ## (level-2 header) section
    # Note: Use '/^## /' with space to match level-2 headers only, not level-3 (###)
    ALERTS_SECTION=$(sed -n '/^## Alerts/,/^## /p' "${OUTPUT_DIR}/e2e-summary.md" | sed '$d' | tail -n +2)

    if [ -n "${ALERTS_SECTION}" ] && [ "${ALERTS_SECTION}" != "None." ]; then
        cat >> "${REPORT_FILE}" << EOF
${ALERTS_SECTION}

Full E2E test summary available at: \`e2e-summary.md\`

EOF
    else
        cat >> "${REPORT_FILE}" << 'EOF'
No Prometheus alerts detected during test execution.

Full E2E test summary available at: `e2e-summary.md`

EOF
    fi
else
    cat >> "${REPORT_FILE}" << 'EOF'

---

## Prometheus Alerts

E2E summary not available. Set `E2E_SUMMARY_OUTPUT` environment variable when running tests to capture alerts.

EOF
fi

# Recommendations
cat >> "${REPORT_FILE}" << 'EOF'

---

## Recommendations

Based on the analysis above, consider:

1. **OpenAPI Schema Caching**: If OpenAPI allocations are significant, implement caching
2. **Informer Optimization**: Review and deduplicate informer creation
3. **List Operation Limits**: Add pagination or field selectors to reduce list overhead
4. **JSON Optimization**: Consider using typed clients instead of unstructured where possible

EOF

log_success "Analysis complete!"
log_info "Report saved to: ${REPORT_FILE}"

# Display summary
echo ""
echo "=== Quick Summary ==="
echo "Test: ${TEST_NAME}"
for component in "${COMPONENTS[@]}"; do
    component_dir="${OUTPUT_DIR}/${component}"
    profile_count=$(find -L "${component_dir}" -maxdepth 1 -name "heap*.pprof" -type f 2>/dev/null | wc -l)
    peak_profile=$(ls -lS "${component_dir}"/heap*.pprof 2>/dev/null | head -1 | awk '{print $NF}')
    peak_name=$(basename "${peak_profile}")
    peak_size=$(du -h "${peak_profile}" | cut -f1)
    echo "${component}: ${profile_count} profiles, peak ${peak_name} (${peak_size})"
done
echo ""
echo "Full report: ${REPORT_FILE}"
