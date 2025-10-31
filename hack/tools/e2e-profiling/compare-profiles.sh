#!/bin/bash
#
# Compare two sets of heap profiles
#
# Usage: compare-profiles.sh <test1> <test2>
#

set -euo pipefail

# Configuration
TEST1="${1:-}"
TEST2="${2:-}"
BASE_DIR="${E2E_PROFILE_DIR:-./e2e-profiles}"
# Convert to absolute paths
if [ -d "${BASE_DIR}" ]; then
    BASE_DIR="$(cd "${BASE_DIR}" && pwd)"
fi
TEST1_DIR="${BASE_DIR}/${TEST1}"
TEST2_DIR="${BASE_DIR}/${TEST2}"
COMPARE_DIR="${BASE_DIR}/comparisons"
REPORT_FILE="${COMPARE_DIR}/${TEST1}-vs-${TEST2}.md"

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

# Validate inputs
if [ -z "${TEST1}" ] || [ -z "${TEST2}" ]; then
    log_error "Usage: $0 <test1> <test2>"
    exit 1
fi

if [ ! -d "${TEST1_DIR}" ]; then
    log_error "Test directory not found: ${TEST1_DIR}"
    exit 1
fi

if [ ! -d "${TEST2_DIR}" ]; then
    log_error "Test directory not found: ${TEST2_DIR}"
    exit 1
fi

# Create comparison directory
mkdir -p "${COMPARE_DIR}"

log_info "Comparing ${TEST1} vs ${TEST2}..."

# Find peak profiles for each test (look in operator-controller subdirectory)
PEAK1=$(ls -lS "${TEST1_DIR}"/operator-controller/heap*.pprof 2>/dev/null | head -1 | awk '{print $NF}')
PEAK2=$(ls -lS "${TEST2_DIR}"/operator-controller/heap*.pprof 2>/dev/null | head -1 | awk '{print $NF}')

# Find peak profiles for catalogd
PEAK1_CATALOGD=$(ls -lS "${TEST1_DIR}"/catalogd/heap*.pprof 2>/dev/null | head -1 | awk '{print $NF}')
PEAK2_CATALOGD=$(ls -lS "${TEST2_DIR}"/catalogd/heap*.pprof 2>/dev/null | head -1 | awk '{print $NF}')

PEAK1_NAME=$(basename "${PEAK1}")
PEAK2_NAME=$(basename "${PEAK2}")
PEAK1_SIZE=$(du -h "${PEAK1}" | cut -f1)
PEAK2_SIZE=$(du -h "${PEAK2}" | cut -f1)

PEAK1_CATALOGD_NAME=$(basename "${PEAK1_CATALOGD}")
PEAK2_CATALOGD_NAME=$(basename "${PEAK2_CATALOGD}")
PEAK1_CATALOGD_SIZE=$(du -h "${PEAK1_CATALOGD}" | cut -f1)
PEAK2_CATALOGD_SIZE=$(du -h "${PEAK2_CATALOGD}" | cut -f1)

# Count profiles for both components
COUNT1=$(find "${TEST1_DIR}/operator-controller" -name "heap*.pprof" -type f 2>/dev/null | wc -l)
COUNT2=$(find "${TEST2_DIR}/operator-controller" -name "heap*.pprof" -type f 2>/dev/null | wc -l)
COUNT1_CATALOGD=$(find "${TEST1_DIR}/catalogd" -name "heap*.pprof" -type f 2>/dev/null | wc -l)
COUNT2_CATALOGD=$(find "${TEST2_DIR}/catalogd" -name "heap*.pprof" -type f 2>/dev/null | wc -l)

log_info "Test 1 operator-controller: ${PEAK1_NAME} (${PEAK1_SIZE}) - ${COUNT1} profiles"
log_info "Test 2 operator-controller: ${PEAK2_NAME} (${PEAK2_SIZE}) - ${COUNT2} profiles"
log_info "Test 1 catalogd: ${PEAK1_CATALOGD_NAME} (${PEAK1_CATALOGD_SIZE}) - ${COUNT1_CATALOGD} profiles"
log_info "Test 2 catalogd: ${PEAK2_CATALOGD_NAME} (${PEAK2_CATALOGD_SIZE}) - ${COUNT2_CATALOGD} profiles"

# Generate comparison report
log_info "Generating comparison report..."

cat > "${REPORT_FILE}" << EOF
# Memory Profile Comparison: ${TEST1} vs ${TEST2}

**Date:** $(date '+%Y-%m-%d %H:%M:%S')

---

## Overview

### operator-controller

| Metric | ${TEST1} | ${TEST2} | Change |
|--------|----------|----------|--------|
| Profiles Collected | ${COUNT1} | ${COUNT2} | $((COUNT2 - COUNT1)) |
| Peak Profile | ${PEAK1_NAME} | ${PEAK2_NAME} | - |
| Peak File Size | ${PEAK1_SIZE} | ${PEAK2_SIZE} | - |

### catalogd

| Metric | ${TEST1} | ${TEST2} | Change |
|--------|----------|----------|--------|
| Profiles Collected | ${COUNT1_CATALOGD} | ${COUNT2_CATALOGD} | $((COUNT2_CATALOGD - COUNT1_CATALOGD)) |
| Peak Profile | ${PEAK1_CATALOGD_NAME} | ${PEAK2_CATALOGD_NAME} | - |
| Peak File Size | ${PEAK1_CATALOGD_SIZE} | ${PEAK2_CATALOGD_SIZE} | - |

---

## Peak Memory Comparison (operator-controller)

### ${TEST1} (Baseline)

\`\`\`
EOF

cd "${TEST1_DIR}/operator-controller" && go tool pprof -top "${PEAK1_NAME}" 2>/dev/null | head -20 >> "${REPORT_FILE}"

cat >> "${REPORT_FILE}" << EOF
\`\`\`

### ${TEST2} (Optimized)

\`\`\`
EOF

cd "${TEST2_DIR}/operator-controller" && go tool pprof -top "${PEAK2_NAME}" 2>/dev/null | head -20 >> "${REPORT_FILE}"

cat >> "${REPORT_FILE}" << EOF
\`\`\`

---

## operator-controller File Size Timeline

| Snapshot | ${TEST1} | ${TEST2} | Difference |
|----------|----------|----------|------------|
EOF

# Get list of all heap numbers from both tests (operator-controller)
ALL_NUMS=$(
    (ls "${TEST1_DIR}"/operator-controller/heap*.pprof 2>/dev/null | sed 's/.*heap\([0-9]*\)\.pprof/\1/';
     ls "${TEST2_DIR}"/operator-controller/heap*.pprof 2>/dev/null | sed 's/.*heap\([0-9]*\)\.pprof/\1/') \
    | sort -n | uniq
)

for num in ${ALL_NUMS}; do
    f1="${TEST1_DIR}/operator-controller/heap${num}.pprof"
    f2="${TEST2_DIR}/operator-controller/heap${num}.pprof"

    if [ -f "${f1}" ]; then
        size1_bytes=$(stat -c%s "${f1}")
        size1_kb=$((size1_bytes / 1024))
        size1="${size1_kb}K"
    else
        size1="-"
        size1_kb=0
    fi

    if [ -f "${f2}" ]; then
        size2_bytes=$(stat -c%s "${f2}")
        size2_kb=$((size2_bytes / 1024))
        size2="${size2_kb}K"
    else
        size2="-"
        size2_kb=0
    fi

    if [ "${size1}" != "-" ] && [ "${size2}" != "-" ]; then
        diff_kb=$((size2_kb - size1_kb))
        if [ $diff_kb -gt 0 ]; then
            diff="+${diff_kb}K"
        elif [ $diff_kb -lt 0 ]; then
            diff="${diff_kb}K"
        else
            diff="0"
        fi
    else
        diff="-"
    fi

    echo "| heap${num}.pprof | ${size1} | ${size2} | ${diff} |" >> "${REPORT_FILE}"
done

cat >> "${REPORT_FILE}" << 'EOF'

---

## catalogd File Size Timeline

| Snapshot | TEST1_NAME | TEST2_NAME | Difference |
|----------|----------|----------|------------|
EOF

# Get list of all catalogd heap numbers from both tests
ALL_NUMS_CATALOGD=$(
    (ls "${TEST1_DIR}"/catalogd/heap*.pprof 2>/dev/null | sed 's/.*heap\([0-9]*\)\.pprof/\1/';
     ls "${TEST2_DIR}"/catalogd/heap*.pprof 2>/dev/null | sed 's/.*heap\([0-9]*\)\.pprof/\1/') \
    | sort -n | uniq
)

for num in ${ALL_NUMS_CATALOGD}; do
    f1="${TEST1_DIR}/catalogd/heap${num}.pprof"
    f2="${TEST2_DIR}/catalogd/heap${num}.pprof"

    if [ -f "${f1}" ]; then
        size1_bytes=$(stat -c%s "${f1}")
        size1_kb=$((size1_bytes / 1024))
        size1="${size1_kb}K"
    else
        size1="-"
        size1_kb=0
    fi

    if [ -f "${f2}" ]; then
        size2_bytes=$(stat -c%s "${f2}")
        size2_kb=$((size2_bytes / 1024))
        size2="${size2_kb}K"
    else
        size2="-"
        size2_kb=0
    fi

    if [ "${size1}" != "-" ] && [ "${size2}" != "-" ]; then
        diff_kb=$((size2_kb - size1_kb))
        if [ $diff_kb -gt 0 ]; then
            diff="+${diff_kb}K"
        elif [ $diff_kb -lt 0 ]; then
            diff="${diff_kb}K"
        else
            diff="0"
        fi
    else
        diff="-"
    fi

    echo "| heap${num}.pprof | ${size1} | ${size2} | ${diff} |" >> "${REPORT_FILE}"
done

cat >> "${REPORT_FILE}" << 'EOF'

---

## operator-controller Analysis

### OpenAPI Allocations Comparison

#### TEST1_NAME (Baseline)

```
EOF

cd "${TEST1_DIR}/operator-controller" && go tool pprof -text "${PEAK1_NAME}" 2>/dev/null | grep -i openapi | head -30 >> "${REPORT_FILE}" || echo "No OpenAPI allocations found" >> "${REPORT_FILE}"

cat >> "${REPORT_FILE}" << 'EOF'
```

#### TEST2_NAME (Optimized)

```
EOF

cd "${TEST2_DIR}/operator-controller" && go tool pprof -text "${PEAK2_NAME}" 2>/dev/null | grep -i openapi | head -30 >> "${REPORT_FILE}" || echo "No OpenAPI allocations found" >> "${REPORT_FILE}"

cat >> "${REPORT_FILE}" << 'EOF'
```

### Growth Analysis Comparison

#### TEST1_NAME Growth (heap0 to peak)

```
EOF

BASELINE1="${TEST1_DIR}/operator-controller/heap0.pprof"
if [ -f "${BASELINE1}" ]; then
    cd "${TEST1_DIR}/operator-controller" && go tool pprof -base="heap0.pprof" -top "${PEAK1}" 2>/dev/null | head -20 >> "${REPORT_FILE}"
else
    echo "Baseline not available" >> "${REPORT_FILE}"
fi

cat >> "${REPORT_FILE}" << 'EOF'
```

#### TEST2_NAME Growth (heap0 to peak)

```
EOF

BASELINE2="${TEST2_DIR}/operator-controller/heap0.pprof"
if [ -f "${BASELINE2}" ]; then
    cd "${TEST2_DIR}/operator-controller" && go tool pprof -base="heap0.pprof" -top "${PEAK2}" 2>/dev/null | head -20 >> "${REPORT_FILE}"
else
    echo "Baseline not available" >> "${REPORT_FILE}"
fi

cat >> "${REPORT_FILE}" << 'EOF'
```

### JSON Deserialization Comparison

#### TEST1_NAME

```
EOF

cd "${TEST1_DIR}/operator-controller" && go tool pprof -text "${PEAK1_NAME}" 2>/dev/null | grep -iE "(json|unmarshal|decode)" | head -20 >> "${REPORT_FILE}" || echo "No JSON operations found" >> "${REPORT_FILE}"

cat >> "${REPORT_FILE}" << 'EOF'
```

#### TEST2_NAME

```
EOF

cd "${TEST2_DIR}/operator-controller" && go tool pprof -text "${PEAK2_NAME}" 2>/dev/null | grep -iE "(json|unmarshal|decode)" | head -20 >> "${REPORT_FILE}" || echo "No JSON operations found" >> "${REPORT_FILE}"

cat >> "${REPORT_FILE}" << 'EOF'
```

### Dynamic Client / Informer Comparison

#### TEST1_NAME

```
EOF

cd "${TEST1_DIR}/operator-controller" && go tool pprof -text "${PEAK1_NAME}" 2>/dev/null | grep -iE "(dynamic|List|Informer|cache)" | head -20 >> "${REPORT_FILE}" || echo "No dynamic client operations found" >> "${REPORT_FILE}"

cat >> "${REPORT_FILE}" << 'EOF'
```

#### TEST2_NAME

```
EOF

cd "${TEST2_DIR}/operator-controller" && go tool pprof -text "${PEAK2_NAME}" 2>/dev/null | grep -iE "(dynamic|List|Informer|cache)" | head -20 >> "${REPORT_FILE}" || echo "No dynamic client operations found" >> "${REPORT_FILE}"

cat >> "${REPORT_FILE}" << 'EOF'
```

---

## catalogd Analysis

### Peak Memory Comparison

#### TEST1_NAME (Baseline)

```
EOF

PEAK1_CATALOGD_NAME=$(basename "${PEAK1_CATALOGD}")
cd "${TEST1_DIR}/catalogd" && go tool pprof -top "${PEAK1_CATALOGD_NAME}" 2>/dev/null | head -20 >> "${REPORT_FILE}"

cat >> "${REPORT_FILE}" << 'EOF'
```

#### TEST2_NAME (Optimized)

```
EOF

PEAK2_CATALOGD_NAME=$(basename "${PEAK2_CATALOGD}")
cd "${TEST2_DIR}/catalogd" && go tool pprof -top "${PEAK2_CATALOGD_NAME}" 2>/dev/null | head -20 >> "${REPORT_FILE}"

cat >> "${REPORT_FILE}" << 'EOF'
```

### Growth Analysis

#### TEST1_NAME Growth (heap0 to peak)

```
EOF

BASELINE1_CATALOGD="${TEST1_DIR}/catalogd/heap0.pprof"
if [ -f "${BASELINE1_CATALOGD}" ]; then
    cd "${TEST1_DIR}/catalogd" && go tool pprof -base="heap0.pprof" -top "${PEAK1_CATALOGD}" 2>/dev/null | head -20 >> "${REPORT_FILE}"
else
    echo "Baseline not available" >> "${REPORT_FILE}"
fi

cat >> "${REPORT_FILE}" << 'EOF'
```

#### TEST2_NAME Growth (heap0 to peak)

```
EOF

BASELINE2_CATALOGD="${TEST2_DIR}/catalogd/heap0.pprof"
if [ -f "${BASELINE2_CATALOGD}" ]; then
    cd "${TEST2_DIR}/catalogd" && go tool pprof -base="heap0.pprof" -top "${PEAK2_CATALOGD}" 2>/dev/null | head -20 >> "${REPORT_FILE}"
else
    echo "Baseline not available" >> "${REPORT_FILE}"
fi

cat >> "${REPORT_FILE}" << 'EOF'
```

---

## Prometheus Alerts Comparison

EOF

# Compare prometheus alerts if available
if [ -f "${TEST1_DIR}/e2e-summary.md" ] || [ -f "${TEST2_DIR}/e2e-summary.md" ]; then
    # Test 1 alerts
    if [ -f "${TEST1_DIR}/e2e-summary.md" ]; then
        # Use '/^## /' with space to match level-2 headers only, not level-3 (###)
        ALERTS1_SECTION=$(sed -n '/^## Alerts/,/^## /p' "${TEST1_DIR}/e2e-summary.md" | sed '$d' | tail -n +2)
        if [ -n "${ALERTS1_SECTION}" ] && [ "${ALERTS1_SECTION}" != "None." ]; then
            ALERTS1="Present"
        else
            ALERTS1="None"
        fi
    else
        ALERTS1="N/A"
    fi

    # Test 2 alerts
    if [ -f "${TEST2_DIR}/e2e-summary.md" ]; then
        # Use '/^## /' with space to match level-2 headers only, not level-3 (###)
        ALERTS2_SECTION=$(sed -n '/^## Alerts/,/^## /p' "${TEST2_DIR}/e2e-summary.md" | sed '$d' | tail -n +2)
        if [ -n "${ALERTS2_SECTION}" ] && [ "${ALERTS2_SECTION}" != "None." ]; then
            ALERTS2="Present"
        else
            ALERTS2="None"
        fi
    else
        ALERTS2="N/A"
    fi

    cat >> "${REPORT_FILE}" << EOF
### Alert Summary

| Metric | ${TEST1} | ${TEST2} |
|--------|----------|----------|
| Alerts | ${ALERTS1} | ${ALERTS2} |

EOF

    if [ "${ALERTS1}" = "Present" ] || [ "${ALERTS2}" = "Present" ]; then
        cat >> "${REPORT_FILE}" << 'EOF'
### Alert Details

EOF

        if [ "${ALERTS1}" = "Present" ]; then
            cat >> "${REPORT_FILE}" << EOF
**${TEST1}:**
${ALERTS1_SECTION}

EOF
        fi

        if [ "${ALERTS2}" = "Present" ]; then
            cat >> "${REPORT_FILE}" << EOF
**${TEST2}:**
${ALERTS2_SECTION}

EOF
        fi
    else
        cat >> "${REPORT_FILE}" << 'EOF'
No alerts detected in either test.

EOF
    fi
else
    cat >> "${REPORT_FILE}" << 'EOF'
E2E summary not available for comparison.

EOF
fi

cat >> "${REPORT_FILE}" << 'EOF'

---

## Key Findings

**Memory Impact:**
- Test duration change: DURATION_CHANGE
- Peak profile size change: SIZE_CHANGE

**Recommendations:**
1. Review the allocation differences above
2. Look for patterns in eliminated allocations
3. Check if optimization goals were met
4. Identify remaining high allocators

---

## Next Steps

Based on this comparison, consider:

1. If memory usage improved: Document the change and create PR
2. If memory usage increased: Investigate unexpected allocations
3. If no change: Review whether optimization was correctly applied

EOF

# Replace placeholders
sed -i "s/TEST1_NAME/${TEST1}/g" "${REPORT_FILE}"
sed -i "s/TEST2_NAME/${TEST2}/g" "${REPORT_FILE}"

# Calculate some statistics
DURATION_CHANGE="$((COUNT2 - COUNT1)) snapshots"
sed -i "s/DURATION_CHANGE/${DURATION_CHANGE}/g" "${REPORT_FILE}"

PEAK1_BYTES=$(stat -c%s "${PEAK1}")
PEAK2_BYTES=$(stat -c%s "${PEAK2}")
DIFF_KB=$(((PEAK2_BYTES - PEAK1_BYTES) / 1024))
if [ $DIFF_KB -gt 0 ]; then
    SIZE_CHANGE="+${DIFF_KB}K (+$(( (DIFF_KB * 100) / (PEAK1_BYTES / 1024) ))%)"
elif [ $DIFF_KB -lt 0 ]; then
    SIZE_CHANGE="${DIFF_KB}K ($(( (DIFF_KB * 100) / (PEAK1_BYTES / 1024) ))%)"
else
    SIZE_CHANGE="No change"
fi
sed -i "s/SIZE_CHANGE/${SIZE_CHANGE}/g" "${REPORT_FILE}"

log_success "Comparison complete!"
log_info "Report saved to: ${REPORT_FILE}"

# Display summary
echo ""
echo "=== Comparison Summary ==="
echo "Test 1: ${TEST1} (${COUNT1} profiles, peak: ${PEAK1_SIZE})"
echo "Test 2: ${TEST2} (${COUNT2} profiles, peak: ${PEAK2_SIZE})"
echo "Duration change: ${DURATION_CHANGE}"
echo "Peak size change: ${SIZE_CHANGE}"
echo ""
echo "Full report: ${REPORT_FILE}"
