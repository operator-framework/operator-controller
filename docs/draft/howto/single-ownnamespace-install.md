## Description

!!! note
This feature is still in *alpha* the `SingleOwnNamespaceInstallSupport` feature-gate must be enabled to make use of it.
See the instructions below on how to enable it.

---

A component of OLMv0's multi-tenancy feature is its support of four [*installModes*](https://olm.operatorframework.io/docs/advanced-tasks/operator-scoping-with-operatorgroups/#targetnamespaces-and-their-relationship-to-installmodes):
for operator installation:

 - *OwnNamespace*: If supported, the operator can be configured to watch for events in the namespace it is deployed in.
 - *SingleNamespace*: If supported, the operator can be configured to watch for events in a single namespace that the operator is not deployed in.
 - *MultiNamespace*: If supported, the operator can be configured to watch for events in more than one namespace.
 - *AllNamespaces*: If supported, the operator can be configured to watch for events in all namespaces.

OLMv1 will not attempt multi-tenancy (see [design decisions document](../../project/olmv1_design_decisions.md)) and will think of operators
as globally installed, i.e. in OLMv0 parlance, as installed in *AllNamespaces* mode. However, there are operators that
were intended only for the *SingleNamespace* and *OwnNamespace* install modes. In order to make these operators installable in v1 while they
transition to the new model, v1 is adding support for these two new *installModes*. It should be noted that, in line with v1's no multi-tenancy policy,
users will not be able to install the same operator multiple times, and that in future iterations of the registry bundle format will not
include *installModes*.

## Demos

### SingleNamespace Install

[![SingleNamespace Install Demo](https://asciinema.org/a/w1IW0xWi1S9cKQFb9jnR07mgh.svg)](https://asciinema.org/a/w1IW0xWi1S9cKQFb9jnR07mgh)

### OwnNamespace Install

[![OwnNamespace Install Demo](https://asciinema.org/a/Rxx6WUwAU016bXFDW74XLcM5i.svg)](https://asciinema.org/a/Rxx6WUwAU016bXFDW74XLcM5i)

## Enabling the Feature-Gate

!!! tip

This guide assumes OLMv1 is already installed. If that is not the case,
you can follow the [getting started](../../getting-started/olmv1_getting_started.md) guide to install OLMv1.

---

Patch the `operator-controller` `Deployment` adding `--feature-gates=SingleOwnNamespaceInstallSupport=true` to the
controller container arguments:

```terminal title="Enable SingleOwnNamespaceInstallSupport feature-gate"
kubectl patch deployment -n olmv1-system operator-controller-controller-manager --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--feature-gates=SingleOwnNamespaceInstallSupport=true"}]'
```

Wait for `Deployment` rollout:

```terminal title="Wait for Deployment rollout"
kubectl rollout status -n olmv1-system deployment/operator-controller-controller-manager
```

## Configuring the `ClusterExtension`

A `ClusterExtension` can be configured to install bundle in `Single-` or `OwnNamespace` mode through the
`.spec.config.inline.watchNamespace` property. The *installMode* is inferred in the following way:

 - *AllNamespaces*: `watchNamespace` is empty, or not set
 - *OwnNamespace*: `watchNamespace` is the install namespace (i.e. `.spec.namespace`)
 - *SingleNamespace*: `watchNamespace` *not* the install namespace

### Examples

``` terminal title="SingleNamespace install mode example"
kubectl apply -f - <<EOF
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  namespace: argocd
  serviceAccount:
    name: argocd-installer
  config:
    inline:
      watchNamespace: argocd-watch
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      version: 0.2.1 # Update to version 0.2.1
    EOF
```

``` terminal title="OwnNamespace install mode example"
kubectl apply -f - <<EOF
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: argocd
  annotations:
    olm.operatorframework.io/watch-namespace: argocd
spec:
  namespace: argocd
  serviceAccount:
    name: argocd-installer
  config:
    inline:
      watchNamespace: argocd
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      version: 0.2.1 # Update to version 0.2.1
    EOF
```
