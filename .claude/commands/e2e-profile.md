---
description: Profile memory and CPU usage during e2e tests and analyze results
---

# E2E Profiling Plugin

Analyze memory and CPU usage during e2e tests by collecting pprof heap and CPU profiles and generating comprehensive analysis reports.

## Commands

### /e2e-profile run [test-name] [test-target]

Run an e2e test with continuous memory and CPU profiling:

1. Start the specified e2e test (defaults to `make test-experimental-e2e`)
2. Wait for the operator-controller pod to be ready
3. Collect heap and CPU profiles every 10 seconds to `./e2e-profiles/[test-name]/`
4. Continue until the test completes or is interrupted
5. Generate a summary report with memory and CPU analysis

**Test Targets:**
- `test-e2e` - Standard e2e tests
- `test-experimental-e2e` - Experimental e2e tests (default)
- `test-extension-developer-e2e` - Extension developer e2e tests
- `test-upgrade-e2e` - Upgrade e2e tests
- `test-upgrade-experimental-e2e` - Upgrade experimental e2e tests

**Examples:**
```
/e2e-profile run baseline
/e2e-profile run baseline test-e2e
/e2e-profile run with-caching test-experimental-e2e
/e2e-profile run upgrade-test test-upgrade-e2e
```

### /e2e-profile analyze [test-name]

Analyze collected heap profiles for a specific test run:

1. Load all heap profiles from `./e2e-profiles/[test-name]/`
2. Analyze memory growth patterns
3. Identify top allocators
4. Find OpenAPI, JSON, and other hotspots
5. Generate detailed markdown report

**Example:**
```
/e2e-profile analyze baseline
```

### /e2e-profile compare [test1] [test2]

Compare two test runs to measure the impact of changes:

1. Load profiles from both test runs
2. Compare peak memory usage
3. Compare memory growth rates
4. Identify differences in allocation patterns
5. Generate side-by-side comparison report with charts

**Example:**
```
/e2e-profile compare baseline with-caching
```

### /e2e-profile collect

Manually collect a single heap profile from the running operator-controller pod:

1. Find the operator-controller pod
2. Set up port forwarding to pprof endpoint
3. Download heap profile
4. Save to `./e2e-profiles/manual/heap-[timestamp].pprof`

**Example:**
```
/e2e-profile collect
```

## Task Breakdown

When you invoke this command, I will:

1. **Setup Phase**
   - Create `./e2e-profiles/[test-name]` directory
   - Verify `make test-experimental-e2e` is available
   - Check kubectl access to the cluster

2. **Collection Phase**
   - Start the e2e test in background
   - Monitor for pod readiness
   - Set up port forwarding to pprof endpoint (port 6060)
   - Collect heap profiles every 10 seconds
   - Save profiles with sequential naming (heap0.pprof, heap1.pprof, ...)

3. **Monitoring Phase**
   - Track test progress
   - Monitor profile file sizes for growth patterns
   - Detect if test crashes or completes

4. **Analysis Phase**
   - Use `go tool pprof` to analyze profiles
   - Extract key metrics:
     - Peak memory usage
     - Memory growth over time
     - Top allocators
     - OpenAPI-related allocations
     - JSON deserialization overhead
     - Informer/cache allocations

5. **Reporting Phase**
   - Generate markdown report with:
     - Executive summary
     - Memory timeline chart
     - Top allocators table
     - Allocation breakdown
     - Recommendations for optimization

## Configuration

The plugin uses these defaults (customizable via environment variables):

```bash
# Namespace where operator-controller runs
E2E_PROFILE_NAMESPACE=olmv1-system

# Collection interval in seconds
E2E_PROFILE_INTERVAL=10

# CPU sampling duration in seconds
E2E_PROFILE_CPU_DURATION=10

# Profile collection mode (both, heap, cpu)
E2E_PROFILE_MODE=both

# Output directory base
E2E_PROFILE_DIR=./e2e-profiles

# Default test target
E2E_PROFILE_TEST_TARGET=test-experimental-e2e
```

**Profile Modes:**
- `both` (default): Collect both heap and CPU profiles
- `heap`: Collect only heap profiles (reduces overhead by ~3%)
- `cpu`: Collect only CPU profiles

## Output Structure

```
e2e-profiles/
├── baseline/
│   ├── operator-controller/
│   │   ├── heap0.pprof
│   │   ├── heap1.pprof
│   │   ├── cpu0.pprof
│   │   ├── cpu1.pprof
│   │   └── ...
│   ├── catalogd/
│   │   ├── heap0.pprof
│   │   ├── cpu0.pprof
│   │   └── ...
│   ├── test.log
│   ├── collection.log
│   └── analysis.md
├── with-caching/
│   └── ...
└── comparisons/
    └── baseline-vs-with-caching.md
```

## Tool Location

The memory profiling scripts are located at:
```
hack/tools/e2e-profiling/
├── e2e-profile.sh        # Main entry point
├── run-profiled-test.sh     # Run test with profiling
├── collect-profiles.sh      # Collect heap profiles
├── analyze-profiles.sh      # Generate analysis
├── compare-profiles.sh      # Compare two runs
├── README.md               # Full documentation
└── USAGE_EXAMPLES.md       # Real-world examples
```

You can run them directly:
```bash
./hack/tools/e2e-profiling/e2e-profile.sh run baseline
./hack/tools/e2e-profiling/e2e-profile.sh analyze baseline
./hack/tools/e2e-profiling/e2e-profile.sh compare baseline optimized
```

## Requirements

- kubectl with access to the cluster
- go tool pprof
- make (for running tests)
- curl (for fetching profiles)
- Port 6060 available for forwarding

## Example Workflow

```bash
# 1. Run baseline test with profiling
/e2e-profile run baseline

# 2. Make code changes (e.g., add caching)
# ... edit code ...

# 3. Run new test with profiling
/e2e-profile run with-caching

# 4. Compare results
/e2e-profile compare baseline with-caching

# 5. Review the comparison report
# Opens: e2e-profiles/comparisons/baseline-vs-with-caching.md
```

## Notes

- The test will run until completion or manual interruption (Ctrl+C)
- Each heap profile is ~11-150KB depending on memory usage
- Analysis requires all heap files to be present
- Port forwarding runs in background and auto-cleans on exit
- Reports are generated in markdown format for easy viewing
