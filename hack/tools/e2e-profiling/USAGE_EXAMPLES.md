# Usage Examples

## Example 1: Analyze Existing Test Data

The repository already has test data from the OpenAPI caching optimization:

```bash
# Set up symlinks to existing data
ln -s /home/tshort/experimental-e2e-testing/test1 e2e-profiles/baseline
ln -s /home/tshort/experimental-e2e-testing/test2 e2e-profiles/with-caching

# Analyze baseline
./hack/tools/e2e-profiling/e2e-profile.sh analyze baseline

# Analyze optimized version
./hack/tools/e2e-profiling/e2e-profile.sh analyze with-caching

# Compare them
./hack/tools/e2e-profiling/e2e-profile.sh compare baseline with-caching
```

**Expected Output:**
- `e2e-profiles/baseline/analysis.md` - Shows ~13 MB OpenAPI allocations
- `e2e-profiles/with-caching/analysis.md` - Shows ~3.5 MB OpenAPI allocations
- `e2e-profiles/comparisons/baseline-vs-with-caching.md` - Shows 73% reduction

## Example 2: Run a New Test

```bash
# Run a new baseline test
./hack/tools/e2e-profiling/e2e-profile.sh run new-baseline

# This will:
# 1. Start make test-experimental-e2e
# 2. Wait for pod to be ready
# 3. Collect profiles every 15s
# 4. Continue until test completes
# 5. Generate analysis automatically
```

**Monitor Progress:**
```bash
# In another terminal, watch profiles being collected
watch -n 2 'ls -lh e2e-profiles/new-baseline/heap*.pprof'

# Check test progress
tail -f e2e-profiles/new-baseline/test.log

# Check collection status
tail -f e2e-profiles/new-baseline/collection.log
```

## Example 3: Quick Manual Collection

If the operator is already running and you just want a snapshot:

```bash
./hack/tools/e2e-profiling/e2e-profile.sh collect
```

This saves to `e2e-profiles/manual/heap-[timestamp].pprof`

## Example 4: Compare Multiple Optimizations

```bash
# Test different caching strategies
./hack/tools/e2e-profiling/e2e-profile.sh run cache-v1
# ... make changes ...
./hack/tools/e2e-profiling/e2e-profile.sh run cache-v2
# ... make changes ...
./hack/tools/e2e-profiling/e2e-profile.sh run cache-v3

# Compare each to baseline
./hack/tools/e2e-profiling/e2e-profile.sh compare baseline cache-v1
./hack/tools/e2e-profiling/e2e-profile.sh compare baseline cache-v2
./hack/tools/e2e-profiling/e2e-profile.sh compare baseline cache-v3

# Compare between versions
./hack/tools/e2e-profiling/e2e-profile.sh compare cache-v1 cache-v2
./hack/tools/e2e-profiling/e2e-profile.sh compare cache-v2 cache-v3
```

## Example 5: Custom Collection Interval

```bash
# Rapid collection (every 5 seconds) for short tests
E2E_PROFILE_INTERVAL=5 \
./hack/tools/e2e-profiling/e2e-profile.sh run rapid-test

# Slow collection (every 60 seconds) for long-running tests
E2E_PROFILE_INTERVAL=60 \
./hack/tools/e2e-profiling/e2e-profile.sh run slow-test
```

## Example 6: Interactive Analysis with go tool pprof

After collecting profiles, dive deeper:

```bash
cd e2e-profiles/baseline

# Interactive mode
go tool pprof heap23.pprof

# Commands you can use in interactive mode:
# top        - Show top allocators
# top20      - Show top 20
# list       - Show source code
# web        - Generate call graph (requires graphviz)
# pdf        - Generate PDF report
# png        - Generate PNG call graph
# quit       - Exit

# One-liners
go tool pprof -top heap23.pprof | head -30
go tool pprof -list=openapi heap23.pprof
go tool pprof -text heap23.pprof | grep -i json

# Compare baseline to peak
go tool pprof -base=heap0.pprof -top heap23.pprof

# Generate visual call graph
go tool pprof -pdf heap23.pprof > callgraph.pdf
```

## Example 7: Focus on Specific Patterns

```bash
# Analyze just OpenAPI allocations
cd e2e-profiles/baseline
go tool pprof -text heap23.pprof | grep -i openapi > openapi-allocations.txt

# Analyze just JSON operations
go tool pprof -text heap23.pprof | grep -iE "(json|unmarshal)" > json-allocations.txt

# Analyze informer overhead
go tool pprof -text heap23.pprof | grep -iE "(informer|cache|watch)" > informer-allocations.txt

# Find all allocations over 1MB
go tool pprof -text heap23.pprof | awk '$1 ~ /[0-9]+kB/ && $1+0 > 1024'
```

## Example 8: Automated Testing Workflow

```bash
#!/bin/bash
# test-optimization.sh

set -e

BASELINE="baseline-$(date +%Y%m%d)"
OPTIMIZED="optimized-$(date +%Y%m%d)"

echo "Running baseline test..."
./hack/tools/e2e-profiling/e2e-profile.sh run "${BASELINE}"

echo "Applying optimization..."
# Apply your code changes here

echo "Building and deploying..."
make docker-build docker-push deploy

echo "Running optimized test..."
./hack/tools/e2e-profiling/e2e-profile.sh run "${OPTIMIZED}"

echo "Comparing results..."
./hack/tools/e2e-profiling/e2e-profile.sh compare "${BASELINE}" "${OPTIMIZED}"

echo "Results:"
cat "e2e-profiles/comparisons/${BASELINE}-vs-${OPTIMIZED}.md"
```

## Example 9: Continuous Monitoring

Set up a cron job or CI pipeline:

```bash
# .github/workflows/e2e-profile.yml
name: Memory Profile
on:
  schedule:
    - cron: '0 2 * * *'  # Daily at 2 AM
  workflow_dispatch:

jobs:
  profile:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Setup cluster
        run: make kind-cluster
      - name: Deploy operator
        run: make deploy
      - name: Run memory profile
        run: ./hack/tools/e2e-profiling/e2e-profile.sh run nightly-$(date +%Y%m%d)
      - name: Upload results
        uses: actions/upload-artifact@v3
        with:
          name: memory-profiles
          path: e2e-profiles/
```

## Example 10: Compare Against Historical Data

```bash
# Keep historical profiles
mkdir -p e2e-profiles/historical

# After each release
cp -r e2e-profiles/release-v1.0 e2e-profiles/historical/
cp -r e2e-profiles/release-v1.1 e2e-profiles/historical/

# Compare releases
./hack/tools/e2e-profiling/e2e-profile.sh compare \
    historical/release-v1.0 \
    historical/release-v1.1
```

## Tips and Tricks

### Tip 1: Quick Peak Finding

```bash
# Find the profile with highest memory usage
ls -lSh e2e-profiles/my-test/heap*.pprof | head -1
```

### Tip 2: Track Memory Growth Rate

```bash
# Show file size progression
for f in e2e-profiles/my-test/heap*.pprof; do
    echo "$(basename $f): $(stat -c%s $f) bytes"
done | column -t
```

### Tip 3: Extract Metrics for Graphing

```bash
# Create CSV of memory over time
echo "snapshot,bytes" > memory-over-time.csv
for f in e2e-profiles/my-test/heap*.pprof; do
    num=$(basename "$f" | sed 's/heap\([0-9]*\).pprof/\1/')
    size=$(stat -c%s "$f")
    echo "$num,$size" >> memory-over-time.csv
done
```

### Tip 4: Alert on Memory Threshold

```bash
# Check if any profile exceeds threshold
THRESHOLD=$((100 * 1024 * 1024))  # 100 MB

for f in e2e-profiles/my-test/heap*.pprof; do
    size=$(stat -c%s "$f")
    if [ $size -gt $THRESHOLD ]; then
        echo "WARNING: $(basename $f) exceeds threshold: $size bytes"
    fi
done
```

### Tip 5: Generate Summary Report

```bash
# Quick summary of all tests
for test in e2e-profiles/*/; do
    if [ -f "$test/analysis.md" ]; then
        test_name=$(basename "$test")
        peak=$(grep "Peak Memory Usage:" "$test/analysis.md" || echo "N/A")
        echo "$test_name: $peak"
    fi
done
```

## Troubleshooting Examples

### Debug Port Forwarding Issues

```bash
# Test manual port forward
kubectl port-forward -n olmv1-system \
    $(kubectl get pod -n olmv1-system -l app.kubernetes.io/name=operator-controller -o name) \
    6060:6060 &

# Test pprof endpoint
curl http://localhost:6060/debug/pprof/

# If that works, try collecting manually
curl http://localhost:6060/debug/pprof/heap > test.pprof
go tool pprof -top test.pprof
```

### Verify Test is Using New Code

```bash
# Check image in deployment
kubectl get deployment -n olmv1-system operator-controller-controller-manager -o jsonpath='{.spec.template.spec.containers[0].image}'

# Check pod is running new image
kubectl get pod -n olmv1-system -l app.kubernetes.io/name=operator-controller -o jsonpath='{.items[0].spec.containers[0].image}'
```

### Clean Up After Failed Test

```bash
# Kill port-forwards
pkill -f "kubectl port-forward.*6060"

# Clean up partial results
rm -rf e2e-profiles/failed-test

# Check for hung processes
ps aux | grep -E "(memory-profile|collect-profiles)"
```
