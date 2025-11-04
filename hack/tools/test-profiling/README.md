# Test Profiling Tools

Collect and analyze heap/CPU profiles during operator-controller tests.

## Quick Start

```bash
# Start profiling
make start-profiling/baseline

# Run tests
make test-e2e

# Stop and analyze
make stop-profiling

# View report
cat test-profiles/baseline/analysis.md

# Compare runs
./bin/test-profile compare baseline optimized
cat test-profiles/comparisons/baseline-vs-optimized.md
```

## Commands

```bash
# Build
make build-test-profiler

# Run test with profiling
./bin/test-profile run <name> [test-target]

# Start/stop daemon
./bin/test-profile start [name]  # Daemonizes automatically
./bin/test-profile stop

# Analyze/compare
./bin/test-profile analyze <name>
./bin/test-profile compare <baseline> <optimized>
./bin/test-profile collect  # Single snapshot
```

## Configuration

```bash
# Define components to profile (optional - defaults to operator-controller and catalogd)
# Format: "name:namespace:deployment:port;name2:namespace2:deployment2:port2"
export TEST_PROFILE_COMPONENTS="operator-controller:olmv1-system:operator-controller-controller-manager:6060;catalogd:olmv1-system:catalogd-controller-manager:6060"

# Profile custom applications
export TEST_PROFILE_COMPONENTS="my-app:my-ns:my-deployment:8080;api-server:api-ns:api-deployment:9090"

# Other settings
export TEST_PROFILE_INTERVAL=10                # seconds between collections
export TEST_PROFILE_CPU_DURATION=10            # CPU profiling duration in seconds
export TEST_PROFILE_MODE=both                  # both|heap|cpu
export TEST_PROFILE_DIR=./test-profiles        # output directory
export TEST_PROFILE_TEST_TARGET=test-e2e       # make target to run
```

**Component Configuration:**
- Each component needs a `/debug/pprof` endpoint (standard Go pprof)
- Local ports are automatically assigned to avoid conflicts
- Default: operator-controller and catalogd in olmv1-system namespace

## Output

```
test-profiles/
├── <name>/
│   ├── operator-controller/{heap,cpu}*.pprof
│   ├── catalogd/{heap,cpu}*.pprof
│   ├── profiler.log
│   └── analysis.md
└── comparisons/<name>-vs-<name>.md
```

## Interactive Analysis

```bash
cd test-profiles/<name>/operator-controller
go tool pprof -top heap23.pprof
go tool pprof -base=heap0.pprof -top heap23.pprof
go tool pprof -text heap23.pprof | grep -i openapi
```
