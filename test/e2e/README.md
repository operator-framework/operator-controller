# E2E Tests - Godog Framework

This directory contains end-to-end (e2e) tests, written using the [Godog](https://github.com/cucumber/godog) framework.

## Overview

### What is Godog/BDD/Cucumber?

Godog is a Behavior-Driven Development (BDD) framework that allows you to write tests in a human-readable format called
[Gherkin](https://cucumber.io/docs/gherkin/reference/). Tests are written as scenarios using Given-When-Then syntax, making them accessible to both technical and
non-technical stakeholders.

**Benefits:**

- **Readable**: Tests serve as living documentation
- **Maintainable**: Reusable step definitions reduce code duplication
- **Collaborative**: Product owners and developers share the same test specifications
- **Structured**: Clear separation between test scenarios and implementation

## Project Structure

Component step packages are introduced incrementally. The layout below shows
their intended ownership after all component extractions land.

```
test/
├── e2e/
│   ├── README.md
│   ├── features_test.go        # Legacy file entry point
│   ├── operator-controller/
│   │   ├── suite_test.go       # Component suite entry point
│   │   └── features/           # ClusterExtension scenarios
│   ├── catalogd/              # Catalog HA, proxy, and TLS scenarios
│   ├── object-controller/     # ClusterObjectSet/Deployment scenarios
│   └── platform/              # Cross-component metrics and demos
└── internal/
    ├── e2e/
    │   ├── runner.go           # Godog options, scenario selection, summary wiring
    │   ├── steps/              # Shared world, hooks, catalog, resource, RBAC, and condition helpers
    │   │   └── testdata/       # RBAC and resource templates
    │   ├── operator-controller/steps/ # ClusterExtension lifecycle and integration assertions
    │   ├── catalogd/steps/     # HA/proxy/TLS steps and restoration hooks
    │   ├── object-controller/steps/ # ClusterObjectSet/Deployment assertions
    │   ├── platform/steps/     # Demo steps and recording
    │   └── support-bundle.yaml # Shared diagnostics
    └── summary/                # Summary generation and embedded templates
```

## Architecture

### 1. Test Runner (`operator-controller/suite_test.go`)

The component entry point registers shared steps, hooks, and its component steps, then calls
`e2e.Run` from `test/internal/e2e` with its feature directory. The shared runner configures Godog options,
scenario selection, and summary generation.

Test helpers live under `test/internal` so their exported functions can be shared
by test suites without becoming public Go APIs. Envtest helpers used by API and
controller unit tests live under `internal/testutil`, which is accessible throughout
the repository.

Features follow the ownership in the [per-component suite design](../../docs/designs/e2e-per-component.md).
The newer `cos_*`, `cod_*`, and `group` features belong to object-controller.
Suites register additional component steps when existing features contain integration
assertions: operator-controller uses object assertions, object-controller retains
ClusterExtension integration coverage, and catalogd proxy tests use ClusterExtension
steps. Platform registers ClusterExtension steps for demos. Feature contents,
scenario names and assertions are preserved. Direct object-controller scenarios
use `@ObjectController` with `@ClusterObjectSet` or `@ClusterObjectDeployment`;
ClusterExtension integration scenarios retain `@BoxcutterRuntime`.

`test/internal/e2e/steps` owns `World` and the `Scenario(ctx)` accessor, generic resource and
condition helpers, catalog/registry prerequisites, RBAC templates, and cleanup.
`ListClusterObjectSetsInGroup` and `ResolveObjectRef` remain shared because
both ClusterExtension and ClusterObjectSet assertions consume them.
Catalogd owns deployment restoration and proxy state. Platform owns recording;
shared helpers observe commands through an optional `CommandRecorder` interface.
Pass extra lifecycle registration to `sharedsteps.RegisterHooks` to keep suite hooks
between shared scenario setup and cleanup. The upgrade helpers remain shared because
catalogd HA scenarios also consume component readiness and catalog reconciliation steps.

### 2. Feature Files (`operator-controller/features/*.feature`)

Gherkin files that describe test scenarios in natural language.

**Structure:**

```gherkin
Feature: [Feature Name]
  [Feature description]

  Background:
  [Common setup steps for all scenarios]

  Scenario: [Scenario Name]
    Given [precondition]
    When [action]
    Then [expected result]
    And [additional assertions]
```

**Example:**

```gherkin
Feature: Install ClusterExtension

  Background:
    Given OLM is available
    And "test" catalog serves bundles
    And Service account "olm-sa" with needed permissions is available in test namespace

  Scenario: Install latest available version from the default channel
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: test
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
        ...
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
```

### 3. Step Definitions (`test/internal/e2e/<component>/steps/steps.go`)

Go functions that implement the steps defined in feature files. Each step is registered with a regex pattern that
matches the Gherkin text.

**Registration:**

```go
func RegisterSteps(sc *godog.ScenarioContext) {
sc.Step(`^bundle "([^"]+)" is installed in version "([^"]+)"$`, BundleInstalled)
sc.Step(`^ClusterExtension is applied$`, shared.ResourceIsApplied)
// ... more steps
}
```

**Step Implementation Pattern:**

Status assertions must read metadata and status from the same API response and
require the asserted condition's `observedGeneration` to equal `metadata.generation`.
Use `shared.WaitForCondition` for condition checks. Bundle metadata uses the `Installed`
condition, and COS phase digests and object counts use the `Ready` condition to
verify that the status reflects the current spec. Deployments instead expose
`status.observedGeneration` at the resource level.

After changing a spec, use a status assertion directly when checking its result;
a separate generation-only wait is redundant. Use
`ClusterExtension has been reconciled the latest generation` before assertions
on other cluster resources when you only need to confirm the spec was processed.
That step accepts any current-generation `Progressing` status or reason, whereas
`ClusterExtension is rolled out` requires `Progressing=True/Succeeded`.

```go
func BundleInstalled(ctx context.Context, name, version string) error {
    sc := shared.Scenario(ctx)
    shared.WaitFor(ctx, func() bool {
        v, err := shared.K8sClient(ctx, "get", "clusterextension", sc.ClusterExtensionName, "-o", "json")
        if err != nil {
          return false
        }
        var ext ocv1.ClusterExtension
        if err := json.Unmarshal([]byte(v), &ext); err != nil {
            return false
        }
        installed := shared.CurrentCondition(ext.Generation, ext.Status.Conditions, ocv1.TypeInstalled)
        return shared.ConditionMatches(installed, metav1.ConditionTrue, nil, nil) &&
            ext.Status.Install.Bundle.Name == name && ext.Status.Install.Bundle.Version == version
    })
    return nil
}
```

### 4. Hooks and Context (`test/internal/e2e/steps/hooks.go`)

Manages test lifecycle and scenario-specific context.

**Hooks:**

- `CheckFeatureTags`: Skips scenarios based on feature gate tags (e.g., `@WebhookProviderCertManager`)
- `CreateScenarioContext`: Creates unique namespace and names for each scenario
- `ScenarioCleanup`: Cleans up resources after each scenario

**Variable Substitution:**

Replaces `${TEST_NAMESPACE}`, `${NAME}`, `${SCENARIO_ID}`, `${PACKAGE:<name>}`, and `${CATALOG:<name>}` with scenario-specific values.

## Writing Tests

### 1. Create a Feature File

Create a new `.feature` file in its owning component's `test/e2e/<component>/features/` directory:

```gherkin
Feature: Your Feature Name
  Description of what this feature tests

  Background:
    Given OLM is available
    And "test" catalog serves bundles

  Scenario: Your scenario description
    When [some action]
    Then [expected outcome]
```

### 2. Implement Step Definitions

Add component assertions in `test/internal/e2e/<component>/steps/`; add reusable prerequisites and helpers in `test/internal/e2e/steps/`:

```go
func RegisterSteps(sc *godog.ScenarioContext) {
    // ... existing steps
    sc.Step(`^your step pattern "([^"]+)"$`, YourStepFunction)
}

func YourStepFunction(ctx context.Context, param string) error {
    sc := shared.Scenario(ctx)
    // Implementation
    return nil
}
```

### 3. Use Existing Steps

Leverage existing steps for common operations:

- **Setup**: `Given OLM is available`, `And "test" catalog serves bundles`
- **Resource Management**: `When ClusterExtension is applied`, `And resource is applied`
- **Assertions**: `Then ClusterExtension is available`, `And bundle "..." is installed`
- **Conditions**: `Then ClusterExtension reports Progressing as True with Reason Retrying:`

### 4. Variable Substitution

Use these variables in YAML templates:

- `${NAME}`: Scenario-specific ClusterExtension name (e.g., `ce-123`)
- `${COS_NAME}`: Scenario-specific ClusterObjectSet name (e.g., `cos-123`; for applying ClusterObjectSets directly)
- `${TEST_NAMESPACE}`: Scenario-specific namespace (e.g., `ns-123`)
- `${SCENARIO_ID}`: Feature-scoped scenario identifier used for resource isolation across concurrent Go suites
- `${PACKAGE:<name>}`: Parameterized package name (e.g., `${PACKAGE:test}` expands to `test-<scenario-id>`)
- `${CATALOG:<name>}`: Catalog resource name (e.g., `${CATALOG:test}` expands to `test-catalog-<scenario-id>`)

### 5. Feature Tags

Tags can be used for different purposes in the test suite:

#### Feature Gate Tags

Use tags to conditionally run scenarios based on feature gates:

```gherkin
@WebhookProviderCertManager
Scenario: Install operator having webhooks
```

Scenarios are skipped if the feature gate is not enabled on the deployed controller.

#### Serial Execution Tag

By default, scenarios run concurrently (up to 100 parallel scenarios). However, some tests must run serially, typically because they:
- Modify shared cluster resources (e.g., cluster-wide TLS configuration)
- Have resource constraints that prevent parallel execution
- Require exclusive access to a resource

To mark a test for serial execution, add the `@Serial` tag:

```gherkin
@Serial
Feature: TLS profile enforcement on metrics endpoints

  Scenario: Test TLS configuration
    Given the "catalogd" deployment is configured with custom TLS settings
    ...
```

The `Makefile` automatically separates scenarios when run without additional `GODOG_ARGS`:
- Scenarios **without** `@Serial` run concurrently in the first test phase
- Scenarios **with** `@Serial` run sequentially in a separate serial test phase

## Running Tests

### Run All Tests

```bash
make test-e2e                      # Standard features
make test-experimental-e2e         # Experimental features
```

### Run Suites Against an Existing Cluster

The aggregate package pattern discovers component suites automatically. Preserve
the two-pass tag policy and disable Go test caching when running it directly:

```bash
go test -count=1 -v ./test/e2e/... -args --godog.tags="~@Serial && ~@demo" --godog.concurrency=100
go test -p 1 -count=1 -v ./test/e2e/... -args --godog.tags="@Serial && ~@demo" --godog.concurrency=1
```

`-p 1` also serializes packages, keeping serial scenarios exclusive as more suites
are added. Demo scenarios run separately through `make update-demos`.

Select `./test/e2e/<component>/...` with the same flags to run just one suite:
`operator-controller`, `catalogd`, `object-controller`, or `platform`. For example:

```bash
go test -count=1 -v ./test/e2e/object-controller/... -args --godog.tags="~@Serial && ~@demo"
```

### Run Standalone Object-Controller Scenarios

Direct scenarios use `Given object-controller is available`. Discovery finds
object-controller in its own namespace; readiness requires its Deployment and
the ClusterObjectSet CRD. Scenarios tagged `@ClusterObjectDeployment` also
require the ClusterObjectDeployment CRD. Permissions come from scenario tags,
independently of feature filenames.

Install only object-controller on a disposable cluster using the existing chart:

```bash
helm template olmv1 ./helm/olmv1 \
  --set options.featureSet=experimental \
  --set options.objectController.enabled=true \
  --set options.operatorController.enabled=false \
  --set options.catalogd.enabled=false | kubectl apply -f -
```

Run direct COS and COD scenarios separately, or select both with
`@ObjectController` (equivalently `@ClusterObjectSet,@ClusterObjectDeployment`;
Godog uses a comma for OR):

```bash
go test -count=1 -v ./test/e2e/object-controller/... -args \
  --e2e.object-controller-only \
  --godog.tags="@ClusterObjectSet && ~@Serial && ~@demo"
go test -count=1 -v ./test/e2e/object-controller/... -args \
  --e2e.object-controller-only \
  --godog.tags="@ClusterObjectDeployment && ~@Serial && ~@demo"
```

Repeat each selector with `@Serial` and `--godog.concurrency=1` for any serial
scenarios. The standalone flag rejects discovered operator-controller/catalogd
Deployments and any installed ClusterExtension/ClusterCatalog CRDs. Missing
object-controller or a required API fails readiness instead of skipping.

The two ClusterExtension integration features and the revision-pruning scenario
in `group.feature` stay in the object-controller suite, following the ownership
design. They retain `@BoxcutterRuntime`, require the integrated installation, and
are excluded by the direct COS/COD selectors. Aggregate standard runs skip direct
object scenarios when object-controller is absent; experimental Boxcutter runs
require it. Existing `@ProgressDeadline`, `@Serial`, and `@demo` filters still apply.
The legacy `go test ./test/e2e/features_test.go` entry point accepts these same
flags and feature paths for downstream Godog jobs.

### Run Standard and Experimental in Parallel

Both suites can run simultaneously on separate KIND clusters (see the Makefile
comment for Linux prerequisites):

```bash
make -j2 test-e2e test-experimental-e2e
```

Custom godog arguments can be modified by setting the following:
```bash
GODOG_ARGS=--godog.tags=@WebhookProviderCertManager make test-experimental-e2e
```

Note that when this is done the `make` target will no longer automatically split the test run into parallel and serial runs, and test execution time may increase. If you wish to add concurrency back into the arguments, it is recommended to also disable the `@Serial` tests:
```bash
GODOG_ARGS="--godog.tags=~@Serial --godog.concurrency=100" make test-experimental-e2e
```

### Iterative Development

For iterating on individual scenarios without full suite setup/teardown each time,
use the persistent cluster and single-scenario targets:

**1. Set up a persistent cluster (once):**

```bash
make e2e-setup                     # Standard features
make experimental-e2e-setup        # Experimental features
```

This builds images, creates a KIND cluster, deploys OLM, and waits for readiness.
The cluster persists until explicitly torn down.

**2. Run individual scenarios:**

```bash
# Run all scenarios in a feature file (its component is discovered automatically)
make e2e/install
make e2e/revision
make e2e/ha

# Run scenarios matching a name prefix (case-insensitive)
make e2e/install/Install                      # all "Install ..." scenarios
make "e2e/install/Install latest"             # prefix with spaces (use quotes)
make e2e/install/Boxcutter                    # single matching scenario

# Override timeout or kubeconfig
make e2e/install/Install E2E_TIMEOUT=30m
make e2e/install KUBECONFIG=~/.kube/config
```

The prefix matches scenario names from the start. If multiple scenarios match, all
of them run. If no scenario matches, the command fails with a list of available
scenario names.

When using `experimental-e2e-setup`, override `KUBECONFIG` to point at the
experimental cluster:

```bash
make e2e/install/Install KUBECONFIG=.kubeconfig/operator-controller-experimental-e2e.kubeconfig
```

**3. Tear down when done:**

```bash
make e2e-teardown                  # Standard cluster
make experimental-e2e-teardown     # Experimental cluster
```

### Run Specific Feature

```bash
go test -count=1 ./test/e2e/operator-controller/... -args features/install.feature
```

### Run Specific Scenario by Tag

```bash
go test -count=1 ./test/e2e/operator-controller/... -args --godog.tags="@WebhookProviderCertManager"
```

### Run with Debug Logging

```bash
go test -count=1 -v ./test/e2e/operator-controller/... -args --log.debug
```

### CLI Options

Godog options can be passed after `-args`:

```bash
go test -count=1 ./test/e2e/operator-controller/... -args \
  --godog.format=pretty \
  --godog.tags="@WebhookProviderCertManager"
```

Available formats: `pretty`, `cucumber`, `progress`, `junit`

**Custom Flags:**

- `--e2e.scenario=<prefix>`: Run scenarios whose name starts with the given prefix (case-insensitive).
  Used by `make e2e/<feature>/<prefix>` internally.
- `--log.debug`: Enable debug logging (development mode)
- `--k8s.cli=<path>`: Specify path to Kubernetes CLI (default: `kubectl`)
  - Useful for using `oc` or a specific kubectl binary

**Example:**

```bash
go test -count=1 ./test/e2e/operator-controller/... -args --log.debug --k8s.cli=oc
```

### Environment Variables

- `KUBECONFIG`: Path to kubeconfig file (defaults to `~/.kube/config`)
- `E2E_SUMMARY_OUTPUT`: Path to write test summary (optional)
- `PROMETHEUS_URL`: Prometheus endpoint for summary generation (set automatically by `make`; skips summary if unset)
- `CLUSTER_REGISTRY_HOST`: In-cluster registry host for pulling catalog images

## Design Patterns

### 1. Scenario Isolation

Each scenario runs in its own namespace with unique resource names, ensuring complete isolation:

- Namespace: `ns-{feature-hash}-{godog-id}`
- ClusterExtension: `ce-{feature-hash}-{godog-id}`

Godog restarts numeric IDs in each process. The feature hash prevents collisions
between concurrent component suites and stays stable when a feature moves. Feature
and scenario annotations retain their existing values.

### 2. Test-Identifying Annotations

Every resource applied during a scenario is automatically annotated with the feature file name and scenario name:

- `e2e.olm.operatorframework.io/feature`: derived from the feature file path (e.g., `install`, `update`, `recover`)
- `e2e.olm.operatorframework.io/scenario`: the scenario name (e.g., `Install latest available version`)

These annotations are added to all resources created within a scenario.

These annotations make it possible to identify which test scenario produced a given resource when debugging failures on a
cluster:

```bash
kubectl get clusterextension -o json | jq '.items[] | {name: .metadata.name, feature: .metadata.annotations["e2e.olm.operatorframework.io/feature"], scenario: .metadata.annotations["e2e.olm.operatorframework.io/scenario"]}'
```

### 3. Automatic Cleanup

The `ScenarioCleanup` hook ensures all resources are deleted after each scenario:

- Deletes ClusterExtensions and (when BoxcutterRuntime gate is enabled) ClusterObjectSets
- Deletes ClusterCatalogs
- Deletes namespaces
- Deletes added resources

### 4. Declarative Resource Management

Resources are managed declaratively using YAML templates embedded in feature files as docstrings:

```gherkin
When ClusterExtension is applied
"""
  apiVersion: olm.operatorframework.io/v1
  kind: ClusterExtension
  metadata:
    name: ${NAME}
  spec:
    ...
  """
```

### 5. Polling with Timeouts

All asynchronous operations use `waitFor` with consistent timeout (300s) and tick (1s):

```go
shared.WaitFor(ctx, func() bool {
    // Check condition
    return conditionMet
})
```

### 6. Feature Gate Detection

Tests automatically detect enabled feature gates from the running controller and skip scenarios that require disabled
features.

## Common Step Patterns

A list of available, implemented steps can be obtained by running:

```shell
go test -count=1 ./test/e2e/operator-controller/... -args --godog.definitions
```

When working with [Claude Code](https://claude.com/claude-code), run the `/list-e2e-steps` command to get a categorized
reference of all step definitions including parameters, DocString expectations, polling behavior, and handler locations.
This is useful when writing new feature files or debugging existing scenarios.

## Best Practices

1. **Keep scenarios focused**: Each scenario should test one specific behavior
2. **Use Background wisely**: Common setup steps belong in Background
3. **Reuse steps**: Leverage existing step definitions before creating new ones
4. **Meaningful names**: Scenario names should clearly describe what is being tested
5. **Avoid implementation details**: Focus on behavior, not implementation

## References

- [Godog Documentation](https://github.com/cucumber/godog)
- [Gherkin Reference](https://cucumber.io/docs/gherkin/reference/)
- [Cucumber Best Practices](https://cucumber.io/docs/guides/10-minute-tutorial/)
