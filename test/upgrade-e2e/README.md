# Upgrade E2E Tests

End-to-end tests that verify OLM upgrades don't break pre-existing ClusterCatalogs and ClusterExtensions.

One scenario follows the same catalog and ClusterExtension through the entire upgrade.
It installs a small generated bundle at version 1.0.0 on standard OLM and records the
extension's UID, live managed resource UIDs, and ready workload pod UIDs before upgrading OLM.
After the upgrade it checks catalog reconciliation and the extension's storage:

- **Standard destination:** the existing extension remains Helm-backed.
- **Experimental destination:** the extension controls a ClusterObjectDeployment (COD),
  which controls migrated revision 1 with collision protection `None` and normal
  revision 2 with `Prevent`. Revision 1 must have completed and be archived;
  revision 2 must be active and ready, with its managed resources under its control.
  COD availability and revision conditions must reflect their current generations.

The scenario verifies the original extension, every original managed resource, and
workload pods retain their UIDs through migration. It then updates that same extension
to 1.0.1. Experimental OLM must produce ready revision 3, archive the earlier revisions,
and preserve the COD and earlier revision UIDs. Standard OLM must update the Helm
release. Stable bundle resource names must keep their UIDs; versioned RBAC resources
and workload pods may be replaced during this extension update.

Feature gates and component namespaces are refreshed from the deployed configuration
after installation and upgrade. Leader leases must point to pods belonging to the
current deployment revision, so stale leases cannot stand in for upgraded controllers.
The scenario starts on standard OLM; experimental-to-experimental upgrades are outside
this coverage. The fixtures use ordinary etcd limits and strict CRD upgrade safety.

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
The targets recreate their named clusters. To preserve an existing investigation cluster,
use a separately named disposable cluster and an explicit kubeconfig with the lower-level runner:

```bash
RELEASE_INSTALL=https://github.com/operator-framework/operator-controller/releases/download/v1.12.0/install.sh \
RELEASE_UPGRADE="$PWD/experimental-install.sh" \
ROOT_DIR="$PWD" KIND_CLUSTER_NAME=upgrade-coverage-test \
KUBECONFIG="$PWD/.kubeconfig/upgrade-coverage-test.kubeconfig" \
go test -count=1 -v ./test/upgrade-e2e/upgrade_test.go
```

First generate the local install scripts and load the locally built images into that cluster.
Set `RELEASE_INSTALL` to the local `standard-install.sh` for local standard-to-experimental
coverage, or `RELEASE_UPGRADE` to it for released standard-to-local standard coverage.
On failure the scenario leaves its resources for inspection. Migration assertions retain
the last observed error and COD conditions, including ownership conflicts, in timeout output.

## Environment Variables

| Variable | Description |
|---|---|
| `RELEASE_INSTALL` | Path or URL to the install script for the stable OLM release |
| `RELEASE_UPGRADE` | Path or URL to the upgrade install script (locally built) |
| `ROOT_DIR` | Project root directory (working directory for install scripts) |
| `KIND_CLUSTER_NAME` | Name of the kind cluster to use |
| `KUBECONFIG` | Path to kubeconfig file (defaults to `~/.kube/config`) |
