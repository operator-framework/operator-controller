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

```
test/e2e/
├── README.md                   # This file
├── features_test.go            # Test runner and suite initialization
├── features/                   # Gherkin feature files
│   ├── install.feature         # ClusterExtension installation scenarios
│   ├── update.feature          # ClusterExtension update scenarios
│   ├── recover.feature         # Recovery scenarios
│   ├── status.feature          # ClusterExtension status scenarios
│   └── metrics.feature         # Metrics endpoint scenarios
└── steps/                      # Step definitions and test utilities
    ├── steps.go                # Step definition implementations
    ├── hooks.go                # Test hooks and scenario context
    └── testdata/               # Test data (RBAC templates)
        ├── serviceaccount-template.yaml
        ├── olm-sa-helm-rbac-template.yaml
        ├── olm-sa-boxcutter-rbac-template.yaml
        ├── pvc-probe-sa-boxcutter-rbac-template.yaml
        ├── cluster-admin-rbac-template.yaml
        └── metrics-reader-rbac-template.yaml
```

## Architecture

### 1. Test Runner (`features_test.go`)

The main test entry point that configures and runs the Godog test suite.

### 2. Feature Files (`features/*.feature`)

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

### 3. Step Definitions (`steps/steps.go`)

Go functions that implement the steps defined in feature files. Each step is registered with a regex pattern that
matches the Gherkin text.

**Registration:**

```go
func RegisterSteps(sc *godog.ScenarioContext) {
sc.Step(`^OLM is available$`, OLMisAvailable)
sc.Step(`^bundle "([^"]+)" is installed in version "([^"]+)"$`, BundleInstalled)
sc.Step(`^ClusterExtension is applied$`, ResourceIsApplied)
// ... more steps
}
```

**Step Implementation Pattern:**

```go
func BundleInstalled(ctx context.Context, name, version string) error {
    sc := scenarioCtx(ctx)
    waitFor(ctx, func() bool {
        v, err := kubectl("get", "clusterextension", sc.clusterExtensionName, "-o", "jsonpath={.status.install.bundle}")
        if err != nil {
          return false
        }
        var bundle map[string]interface{}
        json.Unmarshal([]byte(v), &bundle)
        return bundle["name"] == name && bundle["version"] == version
    })
    return nil
}
```

### 4. Hooks and Context (`steps/hooks.go`)

Manages test lifecycle and scenario-specific context.

**Hooks:**

- `CheckFeatureTags`: Skips scenarios based on feature gate tags (e.g., `@WebhookProviderCertManager`)
- `CreateScenarioContext`: Creates unique namespace and names for each scenario
- `ScenarioCleanup`: Cleans up resources after each scenario

**Variable Substitution:**

Replaces `${TEST_NAMESPACE}`, `${NAME}`, `${SCENARIO_ID}`, `${PACKAGE:<name>}`, and `${CATALOG:<name>}` with scenario-specific values.

## Writing Tests

### 1. Create a Feature File

Create a new `.feature` file in `test/e2e/features/`:

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

Add step implementations in `steps/steps.go`:

```go
func RegisterSteps(sc *godog.ScenarioContext) {
    // ... existing steps
    sc.Step(`^your step pattern "([^"]+)"$`, YourStepFunction)
}

func YourStepFunction(ctx context.Context, param string) error {
    sc := scenarioCtx(ctx)
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
- `${SCENARIO_ID}`: Unique scenario identifier used for resource name isolation
- `${PACKAGE:<name>}`: Parameterized package name (e.g., `${PACKAGE:test}` expands to `test-<scenario-id>`)
- `${CATALOG:<name>}`: Catalog resource name (e.g., `${CATALOG:test}` expands to `test-catalog-<scenario-id>`)

### 5. Feature Tags

Use tags to conditionally run scenarios based on feature gates:

```gherkin
@WebhookProviderCertManager
Scenario: Install operator having webhooks
```

Scenarios are skipped if the feature gate is not enabled on the deployed controller.

## Running Tests

### Run All Tests

```bash
make test-e2e
```

or

```bash
make test-experimental-e2e
```


### Run Specific Feature

```bash
go test test/e2e/features_test.go -- features/install.feature
```

### Run Specific Scenario by Tag

```bash
go test test/e2e/features_test.go --godog.tags="@WebhookProviderCertManager"
```

### Run with Debug Logging

```bash
go test -v test/e2e/features_test.go --log.debug
```

### CLI Options

Godog options can be passed after `--`:

```bash
go test test/e2e/features_test.go \
  --godog.format=pretty \
  --godog.tags="@WebhookProviderCertManager"
```

Available formats: `pretty`, `cucumber`, `progress`, `junit`

**Custom Flags:**

- `--log.debug`: Enable debug logging (development mode)
- `--k8s.cli=<path>`: Specify path to Kubernetes CLI (default: `kubectl`)
  - Useful for using `oc` or a specific kubectl binary

**Example:**

```bash
go test test/e2e/features_test.go --log.debug --k8s.cli=oc
```

### Environment Variables

- `KUBECONFIG`: Path to kubeconfig file (defaults to `~/.kube/config`)
- `E2E_SUMMARY_OUTPUT`: Path to write test summary (optional)
- `CLUSTER_REGISTRY_HOST`: In-cluster registry host for pulling catalog images

## Design Patterns

### 1. Scenario Isolation

Each scenario runs in its own namespace with unique resource names, ensuring complete isolation:

- Namespace: `ns-{scenario-id}`
- ClusterExtension: `ce-{scenario-id}`

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
waitFor(ctx, func() bool {
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
go test test/e2e/features_test.go -d
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
