# Upgrade E2E Tests

End-to-end tests that verify OLM upgrades don't break pre-existing ClusterCatalogs and ClusterExtensions.

These tests use the [Godog](https://github.com/cucumber/godog) BDD framework with shared step definitions
from `test/e2e/steps/`. The upgrade-specific steps live in `test/e2e/steps/upgrade_steps.go`.
See [test/e2e/README.md](../e2e/README.md) for more information on the Godog framework and writing tests.

## Structure

```
test/upgrade-e2e/
├── upgrade_test.go                     # Test runner (reuses shared steps from test/e2e/steps/)
└── features/
    └── operator-upgrade.feature        # Upgrade verification scenarios
```

## Running Tests

OLM ships two deployment profiles:

- **Standard** - core OLM functionality only
- **Experimental** - includes experimental feature gates (e.g., BoxcutterRuntime, WebhookProviderCertManager)

The following Makefile targets run upgrade tests for different upgrade paths:

| Target | Description |
|---|---|
| `make test-upgrade-st2st-e2e` | Upgrade from the latest stable **standard** release to a locally built **standard** build |
| `make test-upgrade-st2ex-e2e` | Upgrade from the latest stable **standard** release to a locally built **experimental** build |
| `make test-st2ex-e2e` | Switchover from a locally built **standard** install to a locally built **experimental** install |

Each target handles kind cluster creation, image building/loading, test execution, and cleanup.

## Environment Variables

| Variable | Description |
|---|---|
| `RELEASE_INSTALL` | Path or URL to the install script for the stable OLM release |
| `RELEASE_UPGRADE` | Path or URL to the upgrade install script (locally built) |
| `ROOT_DIR` | Project root directory (working directory for install scripts) |
| `KIND_CLUSTER_NAME` | Name of the kind cluster to use |
| `KUBECONFIG` | Path to kubeconfig file (defaults to `~/.kube/config`) |
