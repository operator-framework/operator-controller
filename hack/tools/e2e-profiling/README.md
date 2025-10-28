# E2E Profiling Tools

Automated e2e profiling and analysis tools for operator-controller e2e tests.

## Overview

This plugin helps you:
- **Run e2e tests** with automatic profiling
- **Collect heap and CPU profiles** at regular intervals during test execution
- **Analyze memory usage** patterns and identify allocators
- **Analyze CPU performance** bottlenecks and hotspots
- **Compare test runs** to measure optimization impact
- **Generate reports** with actionable insights

## Quick Start

### 1. Run a Test with Profiling

```bash
# Run baseline test
./hack/tools/e2e-profiling/e2e-profile.sh run baseline

# Make code changes...

# Run optimized test
./hack/tools/e2e-profiling/e2e-profile.sh run with-caching
```

### 2. Compare Results

```bash
./hack/tools/e2e-profiling/e2e-profile.sh compare baseline with-caching
```

### 3. View Reports

```bash
# Individual analysis
cat e2e-profiles/baseline/analysis.md

# Comparison
cat e2e-profiles/comparisons/baseline-vs-with-caching.md
```

## Commands

### `run <test-name> [test-target]`

Run an e2e test with continuous e2e profiling.

```bash
# Run with default test (test-experimental-e2e)
./hack/tools/e2e-profiling/e2e-profile.sh run my-test

# Run with specific test target
./hack/tools/e2e-profiling/e2e-profile.sh run my-test test-e2e
./hack/tools/e2e-profiling/e2e-profile.sh run my-test test-upgrade-e2e
```

**Test Targets:**
- `test-e2e` - Standard e2e tests
- `test-experimental-e2e` - Experimental e2e tests (default)
- `test-extension-developer-e2e` - Extension developer e2e tests
- `test-upgrade-e2e` - Upgrade e2e tests
- `test-upgrade-experimental-e2e` - Upgrade experimental e2e tests

**What it does:**
1. Starts the specified make test target in the background
2. Waits for operator-controller and catalogd pods to be ready
3. Collects heap and CPU profiles every 10 seconds
4. Continues until test completes or is interrupted
5. Automatically analyzes results

**Output:**
- `e2e-profiles/my-test/operator-controller/heap*.pprof` - Heap profile snapshots
- `e2e-profiles/my-test/operator-controller/cpu*.pprof` - CPU profile snapshots
- `e2e-profiles/my-test/catalogd/heap*.pprof` - Catalogd heap profiles
- `e2e-profiles/my-test/catalogd/cpu*.pprof` - Catalogd CPU profiles
- `e2e-profiles/my-test/test.log` - Test output
- `e2e-profiles/my-test/collection.log` - Collection log
- `e2e-profiles/my-test/analysis.md` - Automated analysis

### `analyze <test-name>`

Analyze previously collected profiles.

```bash
./hack/tools/e2e-profiling/e2e-profile.sh analyze my-test
```

**What it analyzes:**
- Peak memory usage
- Memory growth patterns
- Top allocators
- OpenAPI-specific allocations
- JSON deserialization overhead
- Dynamic client operations

**Output:**
- `e2e-profiles/my-test/analysis.md` - Detailed report

### `compare <test1> <test2>`

Compare two test runs side-by-side.

```bash
./hack/tools/e2e-profiling/e2e-profile.sh compare baseline optimized
```

**What it compares:**
- Peak memory usage
- File size progression
- Top allocators
- OpenAPI allocations
- JSON operations
- Informer/cache usage

**Output:**
- `e2e-profiles/comparisons/baseline-vs-optimized.md` - Comparison report

### `collect`

Manually collect a single heap profile.

```bash
./hack/tools/e2e-profiling/e2e-profile.sh collect
```

**Use case:** Quick snapshot during manual testing

**Output:**
- `e2e-profiles/manual/heap-[timestamp].pprof`

## Configuration

Set environment variables to customize behavior:

```bash
# Namespace where operator-controller runs
export E2E_PROFILE_NAMESPACE=olmv1-system

# Collection interval in seconds (time between profile snapshots)
# Note: This is the total time including CPU profiling
export E2E_PROFILE_INTERVAL=10

# CPU sampling duration in seconds
# Note: CPU profiles are collected in parallel with heap profiles
export E2E_PROFILE_CPU_DURATION=10

# Profile collection mode (both, heap, cpu)
# both: Collect both heap and CPU profiles (default)
# heap: Collect only heap profiles (reduces overhead)
# cpu:  Collect only CPU profiles
export E2E_PROFILE_MODE=both

# Output directory
export E2E_PROFILE_DIR=./e2e-profiles

# Default test target (if not specified on command line)
export E2E_PROFILE_TEST_TARGET=test-experimental-e2e
```

**Important:** If `E2E_PROFILE_CPU_DURATION` is set to a value greater than or equal to `E2E_PROFILE_INTERVAL`, CPU profiling will continuously run with no gap between samples. For example:
- `INTERVAL=10, CPU_DURATION=10`: CPU profiles continuously, heap snapshots every 10s
- `INTERVAL=20, CPU_DURATION=10`: 10s CPU sample, 10s idle, heap every 20s
- `INTERVAL=5, CPU_DURATION=10`: Warning - CPU profiling takes longer than interval!

## Output Structure

```
e2e-profiles/
├── baseline/
│   ├── operator-controller/
│   │   ├── heap0.pprof          # Initial heap snapshot
│   │   ├── heap1.pprof          # +10s
│   │   ├── cpu0.pprof           # Initial CPU profile
│   │   ├── cpu1.pprof           # +10s
│   │   └── ...
│   ├── catalogd/
│   │   ├── heap0.pprof
│   │   ├── cpu0.pprof
│   │   └── ...
│   ├── test.log                 # Test output
│   ├── collection.log           # Collection log
│   └── analysis.md              # Analysis report
├── with-caching/
│   └── ...
├── manual/
│   └── heap-20251028-113000.pprof
└── comparisons/
    └── baseline-vs-with-caching.md
```

## Integration with Claude Code

Use the `/e2e-profile` slash command in Claude Code:

```
/e2e-profile run baseline
```

Claude Code will:
1. Execute the profiling script
2. Monitor progress
3. Analyze results
4. Present findings
5. Suggest optimizations

## Requirements

- **kubectl**: Access to Kubernetes cluster
- **go**: For `go tool pprof`
- **make**: For running e2e tests
- **curl**: For fetching profiles
- **bash**: Version 4.0+

## Troubleshooting

### No profiles collected

**Problem:** `collection.log` shows connection errors

**Solution:**
1. Check pod is running: `kubectl get pods -n olmv1-system`
2. Verify pprof is enabled (port 6060)
3. Check port forwarding: `kubectl port-forward -n olmv1-system <pod> 6060:6060`

### Test exits early

**Problem:** `test.log` shows test failure before profiling starts

**Solution:**
1. Run test manually first: `make test-experimental-e2e`
2. Fix test issues before profiling
3. Increase initialization wait time

### Analysis fails

**Problem:** `analyze` command errors on pprof

**Solution:**
1. Ensure all heap files are valid: `file e2e-profiles/*/heap*.pprof`
2. Check for empty files: `find memory-profiles -name "*.pprof" -size 0`
3. Verify go tool pprof works: `go tool pprof --help`

### Comparison shows no difference

**Problem:** Both tests show identical memory usage

**Solution:**
1. Verify code changes were built and deployed
2. Check test is actually using new code
3. Ensure both tests ran under same conditions

## Examples

### Example 1: Baseline Measurement

```bash
# Measure current memory usage
./hack/tools/e2e-profiling/e2e-profile.sh run baseline

# Review results
cat e2e-profiles/baseline/analysis.md
```

### Example 2: Test Optimization

```bash
# Run baseline
./hack/tools/e2e-profiling/e2e-profile.sh run before-optimization

# Make code changes
# ... implement caching ...

# Rebuild and redeploy
make docker-build docker-push deploy

# Run optimized test
./hack/tools/e2e-profiling/e2e-profile.sh run after-optimization

# Compare
./hack/tools/e2e-profiling/e2e-profile.sh compare before-optimization after-optimization
```

### Example 4: Heap-Only or CPU-Only Profiling

```bash
# Collect only heap profiles (reduced overhead for memory analysis)
E2E_PROFILE_MODE=heap ./hack/tools/e2e-profiling/e2e-profile.sh run memory-only

# Collect only CPU profiles (for performance analysis)
E2E_PROFILE_MODE=cpu ./hack/tools/e2e-profiling/e2e-profile.sh run cpu-only

# Default: collect both
./hack/tools/e2e-profiling/e2e-profile.sh run both-profiles
```

**Why use heap-only mode:**
- Reduces profiling overhead from ~5-6% to ~2-3%
- More accurate memory measurements (no CPU profiling interference)
- Faster collection cycles
- When you only need memory analysis

**Why use CPU-only mode:**
- Focus on performance bottlenecks
- No heap profiling allocation hooks
- When memory is not a concern

### Example 5: Testing Different Test Suites

```bash
# Test standard e2e
./hack/tools/e2e-profiling/e2e-profile.sh run standard-e2e test-e2e

# Test extension developer e2e
./hack/tools/e2e-profiling/e2e-profile.sh run extension-dev test-extension-developer-e2e

# Test upgrade scenarios
./hack/tools/e2e-profiling/e2e-profile.sh run upgrade test-upgrade-e2e

# Compare different test suites
./hack/tools/e2e-profiling/e2e-profile.sh compare standard-e2e extension-dev
```

### Example 3: Multiple Optimization Attempts

```bash
# Try different approaches
./hack/tools/e2e-profiling/e2e-profile.sh run attempt1-caching
./hack/tools/e2e-profiling/e2e-profile.sh run attempt2-pooling
./hack/tools/e2e-profiling/e2e-profile.sh run attempt3-both

# Compare all against baseline
./hack/tools/e2e-profiling/e2e-profile.sh compare baseline attempt1-caching
./hack/tools/e2e-profiling/e2e-profile.sh compare baseline attempt2-pooling
./hack/tools/e2e-profiling/e2e-profile.sh compare baseline attempt3-both
```

## Best Practices

1. **Always run baseline first**: Establish a reference point before making changes

2. **Use descriptive names**: Use test names that describe what changed
   - ✅ `baseline`, `with-openapi-cache`, `with-informer-limit`
   - ❌ `test1`, `test2`, `final`

3. **Run multiple times**: Memory patterns can vary, run 2-3 times for consistency

4. **Review raw profiles**: Use `go tool pprof` interactively for deep dives
   ```bash
   cd e2e-profiles/baseline
   go tool pprof heap23.pprof
   ```

5. **Keep test conditions consistent**: Same cluster, same data, same duration

6. **Document changes**: Add notes to analysis.md about what changed

## Advanced Usage

### Interactive Analysis

```bash
cd e2e-profiles/my-test

# Top allocators
go tool pprof -top heap23.pprof

# Call graph (requires graphviz)
go tool pprof -pdf heap23.pprof > analysis.pdf

# Interactive mode
go tool pprof heap23.pprof
# Use commands: top, list, web, etc.

# Compare two profiles
go tool pprof -base=heap0.pprof -top heap23.pprof
```

### Custom Collection Interval

```bash
# Collect every 5 seconds
E2E_PROFILE_INTERVAL=5 ./hack/tools/e2e-profiling/e2e-profile.sh run quick-test

# Collect every 60 seconds
E2E_PROFILE_INTERVAL=60 ./hack/tools/e2e-profiling/e2e-profile.sh run long-test
```

### Multiple Namespaces

```bash
# Profile different namespace
E2E_PROFILE_NAMESPACE=my-namespace \
./hack/tools/e2e-profiling/e2e-profile.sh run my-controller-test
```

## Contributing

Improvements welcome! Key areas:

- [x] Add CPU profiling support
- [x] Add separate heap-only and CPU-only modes
- [ ] Add goroutine profiling
- [ ] Support multiple pods (replicas)
- [ ] Add real-time dashboard
- [ ] Support different output formats (JSON, CSV)

## License

See main repository license.

## See Also

- [Go pprof documentation](https://pkg.go.dev/net/http/pprof)
- [Profiling Go Programs](https://go.dev/blog/pprof)
- [Kubernetes kubectl port-forward](https://kubernetes.io/docs/tasks/access-application-cluster/port-forward-access-application-cluster/)
