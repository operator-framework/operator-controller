# ClusterObjectSets

This document explains what ClusterObjectSets are, how they enable safe phased rollouts of Kubernetes resources, and how operator-controller uses them to manage ClusterExtensions.

## What is a ClusterObjectSet?

A `ClusterObjectSet` is a cluster-scoped Kubernetes API that represents a versioned set of Kubernetes resources organized into ordered phases. It provides a declarative way to roll out a group of related resources sequentially, with built-in readiness checks between phases.

The revision content — the `revision` number, `collisionProtection` strategy, and `phases` (including all objects within them) — is immutable once set. This guarantees that the record of what was deployed at a given revision cannot change after creation. Other fields like `lifecycleState` can change over the object's lifecycle (e.g. transitioning from `Active` to `Archived`), and optional fields like `progressDeadlineMinutes` and `progressionProbes` can be configured independently.

Each ClusterObjectSet has:

- A **revision number** — an immutable, sequential integer that identifies the version
- A **lifecycle state** — either `Active` (being reconciled) or `Archived` (inactive); transitions from Active to Archived but not back
- A list of **phases** — immutable, ordered groups of Kubernetes objects that are applied sequentially
- A **collision protection** strategy — immutable, controlling how pre-existing objects are handled
- **Status conditions** — reporting rollout progress, availability, and success

ClusterObjectSets can be used by any controller or system that needs to manage the rollout of a set of Kubernetes resources in a controlled, phased manner. Within OLM, the operator-controller uses ClusterObjectSets as the mechanism to deploy and upgrade ClusterExtensions.

## Why ClusterObjectSets?

ClusterObjectSets solve several problems that arise when managing sets of related Kubernetes resources:

- **Immutable revision content** — The revision number, phases, objects, and collision protection strategy are immutable once set, providing a clear, auditable record of exactly what was deployed at each revision. There is no ambiguity about what resources belong to a given version.
- **Phased rollout** — Resources are grouped into phases (e.g. CRDs before Deployments) and applied sequentially. A phase only progresses after all its objects pass readiness probes, preventing partially-applied states.
- **Safe transitions** — During a revision change, both the old and new revisions remain active until the new revision fully rolls out. Object ownership transitions from one revision to the next, ensuring no resource is left unmanaged.
- **Single ownership** — Each Kubernetes resource can only be managed by one ClusterObjectSet at a time. This prevents conflicts between revisions fighting over the same object.
- **Large resource support** — Object manifests can be stored inline or externalized into Secrets via references, allowing ClusterObjectSets to manage bundles that would otherwise exceed etcd's 1.5 MiB object size limit.

## Lifecycle

### Active state

When a ClusterObjectSet is `Active`, the controller actively reconciles it using the [boxcutter](https://github.com/package-operator/boxcutter) library:

- Phases are applied sequentially, each waiting for readiness before proceeding
- Objects are managed via server-side apply
- Status conditions are updated to reflect rollout progress

### Revision transitions

When transitioning from one revision to the next:

1. A new ClusterObjectSet is created with the next sequential revision number
2. Both old and new revisions remain `Active` during the transition
3. Objects transition ownership from the old revision to the new one
4. Once the new revision reports `Succeeded`, the old revision can be archived

### Archival

When a revision's lifecycle state is set to `Archived`:

- It is removed from the owner list of all previously managed objects
- Objects that did not transition to a succeeding revision are deleted
- The revision cannot be un-archived
- It remains in the cluster for historical reference until garbage collected

## Phases

Objects within a ClusterObjectSet are organized into phases. Each phase groups related resources, and phases are applied sequentially. Within a phase, all objects are applied simultaneously in no particular order.

A phase only progresses to the next after all of its objects pass their readiness probes. This ensures dependencies are satisfied before dependents are created — for example, CRDs are established before Deployments that use custom resources.

Phase names follow the DNS label standard ([RFC 1123](https://tools.ietf.org/html/rfc1123)) and must be unique within a ClusterObjectSet. A ClusterObjectSet supports up to 20 phases, with up to 50 objects per phase.

### Phase ordering in operator-controller

When operator-controller creates a ClusterObjectSet for a ClusterExtension, it automatically assigns objects to phases based on their GroupKind:

| Order | Phase | Resource Kinds |
| --- | --- | --- |
| 1 | `namespaces` | Namespace |
| 2 | `policies` | NetworkPolicy, PodDisruptionBudget, PriorityClass |
| 3 | `identity` | ServiceAccount |
| 4 | `configuration` | Secret, ConfigMap |
| 5 | `storage` | PersistentVolume, PersistentVolumeClaim, StorageClass |
| 6 | `crds` | CustomResourceDefinition |
| 7 | `roles` | ClusterRole, Role |
| 8 | `bindings` | ClusterRoleBinding, RoleBinding |
| 9 | `infrastructure` | Service, Issuer (cert-manager) |
| 10 | `deploy` | Certificate (cert-manager), Deployment |
| 11 | `scaling` | VerticalPodAutoscaler |
| 12 | `publish` | PrometheusRule, ServiceMonitor, PodMonitor, Ingress, Route, ConsoleYAMLSample, ConsoleQuickStart, ConsoleCLIDownload, ConsoleLink, ConsolePlugin |
| 13 | `admission` | ValidatingWebhookConfiguration, MutatingWebhookConfiguration |

Any resource kind not listed above defaults to the `deploy` phase.

!!! note
    This phase ordering is specific to how operator-controller creates ClusterObjectSets. The API itself does not enforce any particular phase ordering — phases are applied in the order they appear in the `spec.phases` list.

## Readiness probes

A phase only progresses to the next after all of its objects pass readiness probes. Several resource kinds have built-in probes:

| Resource Kind | Readiness Criteria |
| --- | --- |
| CustomResourceDefinition | Condition `Established` = True |
| Namespace | `status.phase` = "Active" |
| PersistentVolumeClaim | `status.phase` = "Bound" |
| Deployment | `status.updatedReplicas` == `status.replicas` and condition `Available` = True |
| StatefulSet | `status.updatedReplicas` == `status.replicas` and condition `Available` = True |
| Certificate (cert-manager) | Condition `Ready` = True |
| Issuer (cert-manager) | Condition `Ready` = True |

For custom resources or other objects that need tailored checks, you can define custom progression probes in the `spec.progressionProbes` field. These are experimental and support three assertion types:

`ConditionEqual`
:   Checks that an object has a condition of the specified type and status (e.g. `Ready` = `True`).

`FieldsEqual`
:   Checks that the values at two field paths match (e.g. `spec.replicas` == `status.readyReplicas`).

`FieldValue`
:   Checks that a field has a specific value (e.g. `status.phase` = `"Bound"`).

Probes use selectors to target objects by GroupKind or by label. A probe only runs against objects matching its selector — if no objects in a phase match, the probe is considered to have passed.

## Collision protection

Collision protection controls whether a ClusterObjectSet can adopt pre-existing objects on the cluster. This is configured at three levels, with the most specific taking precedence:

**object > phase > spec**

The available strategies are:

`Prevent`
:   Only manages objects the revision created itself. This is the safest option and prevents ownership collisions entirely.

`IfNoController`
:   Can adopt pre-existing objects that are not owned by another controller. Useful when taking over management of manually-created resources.

`None`
:   Can adopt any pre-existing object, even if owned by another controller. Use with extreme caution — this can cause multiple controllers to fight over the same resource.

## Object storage: inline vs. references

Each object in a phase can be stored in one of two ways (exactly one must be set):

`object`
:   The full Kubernetes manifest is embedded inline in the ClusterObjectSet. Simple and self-contained, but contributes to the overall size of the ClusterObjectSet resource in etcd.

`ref`
:   A reference to a Secret that holds the serialized object manifest. The `ref` specifies the Secret name, namespace, and data key containing either a JSON-encoded manifest or gzip-compressed JSON bytes. This allows ClusterObjectSets to manage bundles that would otherwise exceed etcd's 1.5 MiB object size limit.

When operator-controller creates ClusterObjectSets for ClusterExtensions, it automatically externalizes objects into immutable Secrets:

- Secrets are packed up to 900 KiB each, leaving headroom below the etcd limit
- Objects larger than 900 KiB are gzip-compressed before storage (ref resolution auto-detects and transparently decompresses gzip-compressed values so consumers see the original JSON manifest)
- Content-addressable naming (based on SHA-256 hashes of the data) ensures that Secret names and data keys are deterministic, making creation idempotent and safe to retry

For a detailed design discussion, see [Large Bundle Support](large-bundle-support.md).

## Status conditions

ClusterObjectSets report three conditions that describe their current state:

### Progressing

Indicates whether the revision is actively rolling out.

| Status | Reason | Meaning |
| --- | --- | --- |
| True | `RollingOut` | Actively making progress |
| True | `Retrying` | Encountered a retryable error |
| True | `Succeeded` | Reached the desired state |
| False | `Blocked` | Error requiring manual intervention |
| False | `Archived` | No longer actively reconciled |

### Available

Indicates whether all objects have been successfully rolled out and pass readiness probes.

| Status | Reason | Meaning |
| --- | --- | --- |
| True | `ProbesSucceeded` | All objects pass readiness probes |
| False | `ProbeFailure` | One or more probes failing |
| Unknown | `Reconciling` | Error prevented probe observation |
| Unknown | `Archived` | Objects torn down after archival |
| Unknown | `Migrated` | Migrated from existing release; probes not yet observed |

### Succeeded

A terminal condition set once the rollout completes. It persists even if the revision later becomes unavailable, marking that this version was successfully deployed at least once.

## How operator-controller uses ClusterObjectSets

Within OLM, ClusterObjectSets serve as the deployment mechanism for `ClusterExtensions`. The operator-controller is one consumer of the ClusterObjectSet API.

### Installation

When a ClusterExtension is installed:

1. The operator-controller resolves a bundle from the catalog
2. The bundle's Kubernetes manifests are sorted into phases by resource kind
3. Objects are packed into immutable Secrets
4. A ClusterObjectSet is created with `revision: 1` in `Active` lifecycle state
5. Phases roll out sequentially — each phase waits for all its objects to become ready before the next phase begins

### Upgrades

When a ClusterExtension is upgraded to a new version:

1. A new ClusterObjectSet is created with `revision: N+1`
2. Both old and new revisions are `Active` during the transition
3. Objects transition ownership from the old revision to the new one
4. Once the new revision reports `Succeeded`, the old revision is archived
5. Older archived revisions are garbage collected, keeping the last 5 for auditing

### Relationship with ClusterExtension

A `ClusterExtension` owns one or more `ClusterObjectSet` resources. The ClusterExtension's `.status.activeRevisions` field lists all currently active (non-archived) revisions along with their status conditions.

When created by operator-controller, ClusterObjectSets carry labels that identify the parent:

```yaml
metadata:
  labels:
    olm.operatorframework.io/owner-kind: ClusterExtension
    olm.operatorframework.io/owner-name: my-extension
    olm.operatorframework.io/package-name: my-operator
    olm.operatorframework.io/bundle-version: 1.2.3
```

## Examples

### Inline objects

This example shows a ClusterObjectSet with objects embedded directly in the spec. This is the simplest approach and works well when the total size of all objects stays within etcd's 1.5 MiB limit.

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterObjectSet
metadata:
  name: my-app-rev1
spec:
  revision: 1
  lifecycleState: Active
  collisionProtection: Prevent
  phases:
  - name: crds
    objects:
    - object:
        apiVersion: apiextensions.k8s.io/v1
        kind: CustomResourceDefinition
        metadata:
          name: widgets.example.com
        spec:
          group: example.com
          names:
            kind: Widget
            plural: widgets
          scope: Namespaced
          versions:
          - name: v1
            served: true
            storage: true
            schema:
              openAPIV3Schema:
                type: object
                properties:
                  spec:
                    type: object
  - name: rbac
    objects:
    - object:
        apiVersion: v1
        kind: ServiceAccount
        metadata:
          name: my-app-controller
          namespace: my-app-system
    - object:
        apiVersion: rbac.authorization.k8s.io/v1
        kind: ClusterRole
        metadata:
          name: my-app-manager-role
        rules:
        - apiGroups: ["example.com"]
          resources: ["widgets"]
          verbs: ["get", "list", "watch", "create", "update", "delete"]
  - name: deploy
    objects:
    - object:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: my-app-controller
          namespace: my-app-system
        spec:
          replicas: 1
          selector:
            matchLabels:
              app: my-app-controller
          template:
            metadata:
              labels:
                app: my-app-controller
            spec:
              serviceAccountName: my-app-controller
              containers:
              - name: manager
                image: example.com/my-app-controller:v1.0.0
```

### Secret references

This example shows a ClusterObjectSet that references objects stored in Secrets. This is how operator-controller creates ClusterObjectSets, and is necessary when managing bundles with large resources like CRDs with extensive schemas.

Object manifests are stored in immutable Secrets. Secret names are content-addressable, computed as `<revisionName>-<16 hex chars>` where the suffix is the first 8 bytes of a SHA-256 digest over the Secret's sorted keys and values. Each data key is a 43-character base64url-encoded (no padding) SHA-256 hash of the stored object bytes.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-app-rev1-3a7f1b9c0e2d4f68
  namespace: olmv1-system
immutable: true
data:
  # key: 43-char base64url SHA-256 of the object JSON bytes
  # value: base64-encoded JSON-serialized Kubernetes manifest
  K8sTl2xQa0vNpR7mW4dYcJfHbE9gUiAoZX1sDj6wFCy: <base64-encoded CRD JSON>
---
apiVersion: v1
kind: Secret
metadata:
  name: my-app-rev1-8b2e4a6c1d3f5097
  namespace: olmv1-system
immutable: true
data:
  # Multiple objects can be packed into the same Secret
  Pm5tRqLwXv8sCj3kFdNhYe7bUiA0oZG1x2W9acDfHJy: <base64-encoded ServiceAccount JSON>
  Vn4mTsLwXp8rCk3jFdNhYe7bUiA0oZG1x2W9acDfHQy: <base64-encoded ClusterRole JSON>
  Bq9nRtLxYw2sDk4lGePhZf8cVjB1pUiA0oZH3x5WaCdy: <base64-encoded Deployment JSON>
```

The ClusterObjectSet references these Secrets:

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterObjectSet
metadata:
  name: my-app-rev1
spec:
  revision: 1
  lifecycleState: Active
  collisionProtection: Prevent
  phases:
  - name: crds
    objects:
    - ref:
        name: my-app-rev1-3a7f1b9c0e2d4f68
        namespace: olmv1-system
        key: "K8sTl2xQa0vNpR7mW4dYcJfHbE9gUiAoZX1sDj6wFCy"
  - name: rbac
    objects:
    - ref:
        name: my-app-rev1-8b2e4a6c1d3f5097
        namespace: olmv1-system
        key: "Pm5tRqLwXv8sCj3kFdNhYe7bUiA0oZG1x2W9acDfHJy"
    - ref:
        name: my-app-rev1-8b2e4a6c1d3f5097
        namespace: olmv1-system
        key: "Vn4mTsLwXp8rCk3jFdNhYe7bUiA0oZG1x2W9acDfHQy"
  - name: deploy
    objects:
    - ref:
        name: my-app-rev1-8b2e4a6c1d3f5097
        namespace: olmv1-system
        key: "Bq9nRtLxYw2sDk4lGePhZf8cVjB1pUiA0oZH3x5WaCdy"
```

Multiple objects can reference different keys within the same Secret, allowing efficient packing of smaller objects.

## Inspecting ClusterObjectSets

```bash
# List all ClusterObjectSets
kubectl get clusterobjectsets

# List revisions for a specific extension
kubectl get clusterobjectsets -l olm.operatorframework.io/owner-name=my-extension

# View full details for a specific revision
kubectl get clusterobjectset <name> -o yaml
```

Example output:

```
NAME                   AVAILABLE   PROGRESSING   AGE
my-extension-abc12     Unknown     False         2d
my-extension-def34     True        True          1h
```
