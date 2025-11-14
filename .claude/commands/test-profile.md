---
description: Profile memory and CPU usage during e2e tests and analyze results
---

# Test Profiling

Profile memory and CPU usage during e2e tests using the Go-based `bin/test-profile` tool.

## Available Commands

### /test-profile start [name]
Start background profiling daemon. Collects heap/CPU profiles from operator-controller and catalogd every 10s.
- Auto-stops after 3 consecutive collection failures (e.g., cluster teardown)
- Use with any test command: `make test-e2e`, `make test-experimental-e2e`, etc.
- Follow with `/test-profile stop` to generate analysis

**Example:**
```bash
/test-profile start baseline
make test-e2e
/test-profile stop
```

### /test-profile stop
Stop profiling daemon and generate analysis report. Cleans up port-forwards and empty profiles.

### /test-profile run [name] [test-target]
Automated workflow: start test, profile until completion, analyze.
- Default test-target: `test-e2e`
- Other targets: `test-experimental-e2e`, `test-upgrade-e2e`, etc.

**Example:**
```bash
/test-profile run baseline test-e2e
```

### /test-profile analyze [name]
Analyze existing profiles in `test-profiles/[name]/`. Generates markdown report with:
- Memory/CPU growth patterns
- Top allocators
- OpenAPI, JSON, and cache hotspots

### /test-profile compare [baseline] [optimized]
Compare two test runs. Outputs to `test-profiles/comparisons/[baseline]-vs-[optimized].md`

### /test-profile collect
One-time snapshot of heap/CPU profiles from running pods. Saves to `test-profiles/manual/`

## Implementation Steps

When executing commands, I will:

1. **Build tool**: `make build-test-profiler` (builds to `bin/test-profile`)
2. **Execute command**: `./bin/test-profile [command] [args]`
3. **For start/stop workflow**: Monitor daemon logs, handle errors gracefully
4. **For run command**: Start test in background, monitor progress, analyze on completion
5. **For analysis**: Present key findings from generated markdown reports

## Configuration

Environment variables (defaults shown):
```bash
TEST_PROFILE_COMPONENTS="operator-controller:olmv1-system:operator-controller-controller-manager:6060;catalogd:olmv1-system:catalogd-controller-manager:6060"
TEST_PROFILE_INTERVAL=10           # seconds between collections
TEST_PROFILE_CPU_DURATION=10       # CPU profiling duration
TEST_PROFILE_MODE=both             # both|heap|cpu
TEST_PROFILE_DIR=./test-profiles
TEST_PROFILE_TEST_TARGET=test-e2e
```

## Output Structure

```
test-profiles/
├── [name]/
│   ├── operator-controller/
│   │   ├── heap*.pprof
│   │   └── cpu*.pprof
│   ├── catalogd/
│   │   ├── heap*.pprof
│   │   └── cpu*.pprof
│   ├── profiler.log
│   └── analysis.md
└── comparisons/
    └── [name1]-vs-[name2].md
```

## Tool Location

- Source: `hack/tools/test-profiling/` (Go-based CLI)
- Binary: `bin/test-profile`
- Make targets: `make build-test-profiler`, `make start-profiling/[name]`, `make stop-profiling`

## Requirements

- kubectl access to cluster
- go tool pprof (for analysis)
- Go version minimum from `hack/tools/test-profiling/go.mod` (for building)
