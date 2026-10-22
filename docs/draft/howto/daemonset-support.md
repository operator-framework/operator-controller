## Description

!!! note
This feature is still in *alpha*. The `RegistryV1DaemonSetSupport` feature-gate is disabled by default and must be enabled to make use of it.
It has no effect unless the `BoxcutterRuntime` feature-gate is also enabled.
See the instructions below on how to enable it.

---

Some operators need an agent on every node, for example a CSI driver's node plugin. Without this feature-gate,
OLMv1 rejects any registry+v1 bundle that includes a `DaemonSet` in its `manifests/` directory:

```
bundle contains unsupported resource: Name: <name>, Kind: DaemonSet
```

With the feature-gate enabled, a `DaemonSet` in `manifests/` is installed into the `ClusterExtension`'s install
namespace, like any other namespaced object in the bundle.

The `DaemonSet` takes part in rollout progression: the
revision is not considered rolled out until `status.updatedNumberScheduled` and `status.numberAvailable` both equal
`status.desiredNumberScheduled` for the latest observed generation.

!!! note
As with the `Deployment` progression probe, a `DaemonSet` whose status reports zero scheduled pods does not pass
the probe, because the API server omits zero-valued `updatedNumberScheduled` and `numberAvailable` fields.

## Enabling the Feature-Gate

!!! tip

This guide assumes OLMv1 is already installed. If that is not the case,
you can follow the [getting started](../../getting-started/olmv1_getting_started.md) guide to install OLMv1.

---

Patch the `operator-controller` `Deployment` adding `--feature-gates=RegistryV1DaemonSetSupport=true` to the
controller container arguments. `BoxcutterRuntime` must be enabled as well:

```terminal title="Enable RegistryV1DaemonSetSupport feature-gate"
kubectl patch deployment -n olmv1-system operator-controller-controller-manager --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--feature-gates=RegistryV1DaemonSetSupport=true"}]'
```

Wait for `Deployment` rollout:

```terminal title="Wait for Deployment rollout"
kubectl rollout status -n olmv1-system deployment/operator-controller-controller-manager
```
