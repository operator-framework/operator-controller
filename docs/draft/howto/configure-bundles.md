## Description

!!! note
This feature is still in `alpha` and the `SingleOwnNamespaceInstallSupport` feature-gate must be enabled to make use of it.
See the instructions below on how to enable it.

---

# Configuring OLM v1 Extensions: Migration and Reference

In OLM v1, the way extensions are configured has changed significantly to improve flexibility and consistency. This guide explains the architectural shift from OLM v0, how to inspect bundles for supported configurations, and how to correctly configure `registry+v1` (legacy) bundles using the new `ClusterExtension` API.

## OLM v0 vs. OLM v1: The Configuration Shift

In **OLM v0**, configuration was split across multiple resources and concepts. "Install Modes" (defining which namespaces an Operator watches) were handled by the `OperatorGroup` resource, while operand configuration (environment variables, resource limits) was handled via the `Subscription` resource.

In **OLM v1**, these concepts are unified under the **ClusterExtension** resource.

| Feature             | OLM v0 Approach                                                                                           | OLM v1 Approach                                                                                                                    |
|:--------------------|:----------------------------------------------------------------------------------------------------------|:-----------------------------------------------------------------------------------------------------------------------------------|
| **Namespace Scope** | Defined by **OperatorGroup**. You had to pre-create an OperatorGroup to tell the Operator where to watch. | Defined by **Configuration**. You provide a `watchNamespace` value directly in the `ClusterExtension` YAML.                        |
| **User Settings**   | `Subscription.spec.config` (e.g., env, resources).                                                        | `ClusterExtension.spec.config.inline` (arbitrary JSON/YAML defined by the bundle author).                                          |
| **Multi-Tenancy**   | "Install Modes" allowed multiple instances of an operator to exist.                                       | "Install Modes" are treated as **bundle configuration**. You install the extension once, and configure it to watch specific areas. |

### The `watchNamespace` Configuration

For existing `registry+v1` bundles (standard OLM bundles), OLM v1 automatically generates a configuration schema based on the bundle's capabilities. The primary configuration field available is `watchNamespace`.

* **OLM v0:** You selected `SingleNamespace` mode by creating an `OperatorGroup` that targeted a specific namespace.
* **OLM v1:** You set `watchNamespace: "my-target-namespace"` inside the `ClusterExtension` config.

## Step 1: Identifying Bundle Capabilities

Before configuring a bundle, you must understand which Install Modes it supports. OLM v1 does not allow you to force a configuration that the bundle author has not explicitly supported.

You can inspect a bundle image using the `opm` CLI tool and `jq` to parse the output.

**Prerequisites:**
* `opm` CLI installed.
* `jq` installed.

**Command:**

Run the following command, replacing `<bundle-image>` with your target image (e.g., `quay.io/example/my-operator-bundle:v1.0.0`).

```bash
opm render <bundle-image> -o json | \
jq 'select(.schema == "olm.bundle") | .properties[] | select(.type == "olm.csv") | .value.spec.installModes'
```

**Example Output:**

```json
[
  {
    "type": "OwnNamespace",
    "supported": true
  },
  {
    "type": "SingleNamespace",
    "supported": true
  },
  {
    "type": "MultiNamespace",
    "supported": false
  },
  {
    "type": "AllNamespaces",
    "supported": false
  }
]
```

By analyzing which modes are marked `true`, you can determine how to configure the `ClusterExtension` in the next step.

## Step 2: Capability Matrix & Configuration Guide

Use the output from Step 1 to locate your bundle's capabilities in the matrix below. This determines if you *must* provide configuration, if it is optional, and what values are valid.

### Legend
* **Install Namespace:** The namespace where the Operator logic (Pod) runs (defined in `ClusterExtension.spec.namespace`).
* **Watch Namespace:** The namespace the Operator monitors for Custom Resources (defined in `spec.config.inline.watchNamespace`).

### Configuration Matrix

| Capabilities Detected (from `opm`)       | `watchNamespace` Field Status | Valid Values / Constraints                                                                                |
|:-----------------------------------------|:------------------------------|:----------------------------------------------------------------------------------------------------------|
| **OwnNamespace ONLY**                    | **Required**                  | Must be exactly the same as the **Install Namespace**.                                                    |
| **SingleNamespace ONLY**                 | **Required**                  | Must be **different** from the Install Namespace.                                                         |
| **OwnNamespace** AND **SingleNamespace** | **Required**                  | Can be **any** namespace (either the install namespace or a different one).                               |
| **AllNamespaces** (regardless of others) | **Optional**                  | If omitted: defaults to Cluster-wide watch.<br>If provided: can be any specific namespace (limits scope). |

### Common Configuration Scenarios

#### Scenario A: The Legacy "OwnNamespace" Operator
* **Capability:** Only supports `OwnNamespace`.
* **Requirement:** The operator is hardcoded to watch its own namespace.
* **Config:** You must explicitly set `watchNamespace` to match the installation namespace.

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: my-operator
spec:
  namespace: my-operator-ns      # <--- Install Namespace
  serviceAccount:
    name: my-sa
  config:
    configType: Inline
    inline:
      watchNamespace: my-operator-ns # <--- MUST match Install Namespace
  source:
    sourceType: Catalog
    catalog:
      packageName: my-package
```

#### Scenario B: The "SingleNamespace" Operator
* **Capability:** Supports `SingleNamespace` (but not `OwnNamespace`).
* **Requirement:** The operator runs in one namespace (e.g., `ops-system`) but watches workloads in another (e.g., `dev-team-a`).
* **Config:** You must set `watchNamespace` to the target workload namespace.

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: monitor-operator
spec:
  namespace: ops-system          # <--- Install Namespace
  serviceAccount:
    name: monitor-operator-installer
  source:
    sourceType: Catalog
    catalog:
      packageName: monitor-operator
  config:
    configType: Inline
    inline:
      watchNamespace: dev-team-a     # <--- MUST differ from Install Namespace
```

#### Scenario C: The Modern "AllNamespaces" Operator
* **Capability:** Only supports `AllNamespaces`.

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: global-operator
spec:
  namespace: operators
  # No config provided = Operator watches the entire cluster (AllNamespaces)
  serviceAccount:
    name: global-operator-installer
  source:
    sourceType: Catalog
    catalog:
      packageName: global-operator
```


## Troubleshooting Configuration Errors

OLM v1 validates your configuration against the bundle's schema before installation proceeds. If your configuration is invalid, the `ClusterExtension` will report a `Progressing` condition with an error message.

| Error Message Example                                                                                                                                                         | Cause                                                                                              | Solution                                                                                                        |
|:------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:---------------------------------------------------------------------------------------------------|:----------------------------------------------------------------------------------------------------------------|
| `required field "watchNamespace" is missing`                                                                                                                                  | The bundle does not support `AllNamespaces` default mode.                                          | Add the `inline` block and specify a `watchNamespace`.                                                          |
| `invalid value "X": watchNamespace must be "Y" (the namespace where the operator is installed) because this operator only supports OwnNamespace install mode`                 | You tried to set a different watch namespace for an `OwnNamespace`-only bundle.                    | Change `watchNamespace` to match `spec.namespace`.                                                              |
| `invalid value "X": watchNamespace must be different from "Y" (the install namespace) because this operator uses SingleNamespace install mode to watch a different namespace` | You tried to set the watch namespace to the install namespace for a `SingleNamespace`-only bundle. | Change `watchNamespace` to a different target namespace.                                                        |
| `unknown field "foo"`                                                                                                                                                         | You added extra fields to the inline config.                                                       | Remove fields other than `watchNamespace` (unless the bundle author explicitly documents extra schema support). |
