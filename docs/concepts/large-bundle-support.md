# Design: Large Bundle Support

## Need

ClusterExtensionRevision (CER) objects embed full Kubernetes manifests inline in
`.spec.phases[].objects[].object`. With up to 20 phases and 50 objects per phase,
the serialized CER can approach or exceed the etcd maximum object size of
1.5 MiB. Large operators shipping many CRDs, Deployments, RBAC rules, and
webhook configurations are likely to hit this limit.

When the limit is exceeded, the API server rejects the CER and the extension
cannot be installed or upgraded. Today there is no mitigation path other than
reducing the number of objects in the bundle.

The phases data is immutable after creation, write-once/read-many, and only
consumed by the revision reconciler — making it a good candidate for
externalization.

This document presents two approaches for solving this problem. Per-object
content references is the preferred approach; externalized phases to Secret
chains is presented as an alternative.

---

## Approach: Per-Object Content References

Externalize all objects by default. Add an optional `ref` field to
`ClusterExtensionRevisionObject` that points to the object content stored in a
Secret. Exactly one of `object` or `ref` must be set. The system uses `ref` for
all objects it creates; users who manually craft CERs may use either `object` or
`ref`.

### API change

Add a new `ObjectSourceRef` type and a `ref` field to
`ClusterExtensionRevisionObject`. Exactly one of `object` or `ref` must be set,
enforced via CEL validation. Both fields are immutable (inherited from the
immutability of phases).

```go
type ClusterExtensionRevisionObject struct {
	// object is an optional embedded Kubernetes object to be applied.
	// Exactly one of object or ref must be set.
	//
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Object *unstructured.Unstructured `json:"object,omitempty"`

	// ref is an optional reference to a Secret that holds the serialized
	// object manifest.
	// Exactly one of object or ref must be set.
	//
	// +optional
	Ref *ObjectSourceRef `json:"ref,omitempty"`

	// collisionProtection controls whether the operator can adopt and modify
	// objects that already exist on the cluster.
	//
	// +optional
	// +kubebuilder:validation:Enum=Prevent;IfNoController;None
	CollisionProtection CollisionProtection `json:"collisionProtection,omitempty"`
}
```

CEL validation on `ClusterExtensionRevisionObject`:

```
rule: "has(self.object) != has(self.ref)"
message: "exactly one of object or ref must be set"
```

#### ObjectSourceRef

```go
// ObjectSourceRef references content within a Secret that contains a
// serialized object manifest.
type ObjectSourceRef struct {
	// name is the name of the referenced Secret.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// namespace is the namespace of the referenced Secret.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`

	// key is the data key within the referenced Secret containing the
	// object manifest content. The value at this key must be a
	// JSON-serialized Kubernetes object manifest.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Key string `json:"key"`
}
```

### Content format

The content at the referenced key is a JSON-serialized Kubernetes manifest — the
same structure currently used inline in the `object` field.

Content may optionally be gzip-compressed. The reconciler auto-detects
compression by inspecting the first two bytes of the content: gzip streams
always start with the magic bytes `\x1f\x8b`. If detected, the content is
decompressed before JSON deserialization. Otherwise, the content is treated as
plain JSON. This makes compression transparent — no additional API fields or
annotations are needed, and producers can choose per-key whether to compress.

Kubernetes manifests are highly repetitive structured text and typically achieve
5-10x size reduction with gzip, following the same pattern used by Helm for
release storage.

To inspect an uncompressed referenced object stored in a Secret:

```sh
kubectl get secret <name> -o jsonpath='{.data.<key>}' | base64 -d | jq .
```

To inspect a gzip-compressed referenced object stored in a Secret:

```sh
kubectl get secret <name> -o jsonpath='{.data.<key>}' | base64 -d | gunzip | jq .
```

### Referenced resource conventions

The CER API does not enforce any particular structure or metadata on the
referenced Secret — the `ref` field is a plain pointer. The reconciler only
requires that the Secret exists and that the key resolves to valid JSON content
(optionally gzip-compressed). Everything else is a convention that the system
follows when it creates referenced Secrets, and that other producers should
follow for consistency and safe lifecycle management.

Recommended conventions:

1. **Immutability**: Secrets should set `immutable: true`. Because CER phases
   are immutable, the content backing a ref should not change after creation.
   Mutable referenced Secrets are not rejected, but modifying them after the
   CER is created leads to undefined behavior.

2. **Owner references**: Referenced Secrets should carry an ownerReference to
   the CER so that Kubernetes garbage collection removes them when the CER is
   deleted:
   ```yaml
   ownerReferences:
     - apiVersion: olm.operatorframework.io/v1
       kind: ClusterExtensionRevision
       name: <CER-name>
       uid: <revision-uid>
       controller: true
   ```
   Without an ownerReference, the producer is responsible for cleaning up the
   Secret when the CER is deleted. The reconciler does not delete referenced
   Secrets itself.

3. **Revision label**: A label identifying the owning revision aids discovery,
   debugging, and bulk cleanup:
   ```
   olm.operatorframework.io/revision-name: <CER-name>
   ```
   This enables fetching all referenced Secrets for a revision with a single
   list call:
   ```sh
   kubectl get secrets -l olm.operatorframework.io/revision-name=my-extension-1
   ```

Multiple externalized objects may share a single Secret by using different keys.
This reduces the number of Secrets created.

### Controller implementation

The ClusterExtension controller externalizes all objects by default. When the
Boxcutter applier creates a CER, every object entry uses `ref` pointing to the
object content stored in a Secret. Secrets are created in the system namespace
(`olmv1-system` by default). This keeps CERs uniformly small regardless of
bundle size.

Users who manually craft CERs may use either inline `object` or `ref` pointing
to their own Secrets. Inline `object` is convenient for development, testing, or
extensions with very few small objects. Users who prefer to manage their own
externalized storage can create Secrets and use `ref` directly.

### Example

A system-created CER with all objects externalized:

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtensionRevision
metadata:
  name: my-extension-1
spec:
  revision: 1
  lifecycleState: Active
  collisionProtection: Prevent
  phases:
    - name: rbac
      objects:
        - ref:
            name: my-extension-1-rbac
            namespace: olmv1-system
            key: service-account
        - ref:
            name: my-extension-1-rbac
            namespace: olmv1-system
            key: cluster-role
    - name: crds
      objects:
        - ref:
            name: my-extension-1-crds
            namespace: olmv1-system
            key: my-crd
    - name: deploy
      objects:
        - ref:
            name: my-extension-1-deploy
            namespace: olmv1-system
            key: deployment
---
apiVersion: v1
kind: Secret
metadata:
  name: my-extension-1-rbac
  namespace: olmv1-system
  labels:
    olm.operatorframework.io/revision-name: my-extension-1
  ownerReferences:
    - apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtensionRevision
      name: my-extension-1
      uid: <revision-uid>
      controller: true
immutable: true
data:
  service-account: <base64(JSON ServiceAccount manifest)>
  cluster-role:    <base64(JSON ClusterRole manifest)>
---
apiVersion: v1
kind: Secret
metadata:
  name: my-extension-1-crds
  namespace: olmv1-system
  labels:
    olm.operatorframework.io/revision-name: my-extension-1
  ownerReferences:
    - apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtensionRevision
      name: my-extension-1
      uid: <revision-uid>
      controller: true
immutable: true
data:
  my-crd: <base64(JSON CRD manifest)>
---
apiVersion: v1
kind: Secret
metadata:
  name: my-extension-1-deploy
  namespace: olmv1-system
  labels:
    olm.operatorframework.io/revision-name: my-extension-1
  ownerReferences:
    - apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtensionRevision
      name: my-extension-1
      uid: <revision-uid>
      controller: true
immutable: true
data:
  deployment: <base64(JSON Deployment manifest)>
```

A user-crafted CER with inline objects:

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtensionRevision
metadata:
  name: my-extension-1
spec:
  revision: 1
  lifecycleState: Active
  collisionProtection: Prevent
  phases:
    - name: deploy
      objects:
        - object:
            apiVersion: apps/v1
            kind: Deployment
            metadata:
              name: my-operator
              namespace: my-ns
            spec: { ... }
```

### Packing strategy

The ClusterExtension controller packs externalized objects into Secrets in the
system namespace (`olmv1-system` by default):

1. Iterate over all objects across all phases in order. For each object,
   serialize it as JSON, compute its content hash
   (SHA-256, [base64url](https://datatracker.ietf.org/doc/html/rfc4648#section-5)-encoded
   without padding — 43 characters), and store it in the current Secret using
   the hash as the data key. The corresponding `ref.key` is set to the hash.
   Using the content hash as key ensures that it is a stable, deterministic
   identifier tied to the exact content — if the content changes, the hash
   changes, which in turn changes the key in the CER, guaranteeing that any
   content mutation is visible as a CER spec change and triggers a new
   revision.
2. When adding an object would push the current Secret beyond 900 KiB (leaving
   headroom for base64 overhead and metadata), finalize it and start a new one.
   Objects from different phases may share the same Secret.
3. If a single serialized object exceeds the Secret size limit on its own,
   creation fails with a clear error.

Multiple Secrets are independent — each is referenced directly by a `ref` in
the CER. There is no linked-list chaining between them.

### Crash-safe creation sequence

The creation sequence ensures that by the time the CER exists and is visible to
the CER reconciler, all referenced Secrets are already present. This avoids
spurious "not found" errors and unnecessary retry loops.

ownerReferences require the parent's `uid`, which is only assigned by the API
server at creation time. Since we must create the Secrets before the CER, they
are initially created without ownerReferences. The Secret `immutable` flag only
protects `.data` and `.stringData` — metadata (including ownerReferences) can
still be patched after creation.

```
Step 1: Create Secret(s) with revision label, no ownerReference
             |
             |  crash here → Orphaned Secrets exist with no owner.
             |                ClusterExtension controller detects them on
             |                next reconciliation by listing Secrets with
             |                the revision label and checking whether the
             |                corresponding CER exists. If not, deletes them.
             v
Step 2: Create CER with refs pointing to the Secrets from step 1
             |
             |  crash here → CER exists, Secrets exist, refs resolve.
             |                CER reconciler can proceed normally.
             |                Secrets have no ownerReferences yet.
             |                ClusterExtension controller retries step 3.
             v
Step 3: Patch ownerReferences onto Secrets (using CER uid)
             |
             |  crash here → Some Secrets have ownerRefs, some don't.
             |                ClusterExtension controller retries patching
             |                the remaining Secrets on next reconciliation.
             v
         Done — CER has refs, all Secrets exist with owner refs.
```

Key properties:
- **No reconciler churn**: Referenced Secrets exist before the CER is created.
  The CER reconciler never encounters missing Secrets during normal operation.
- **Orphan cleanup**: Secrets created in step 1 carry the revision label
  (`olm.operatorframework.io/revision-name`). If a crash leaves Secrets without
  a corresponding CER, the ClusterExtension controller detects and deletes them
  on its next reconciliation.
- **Idempotent retry**: Secrets are immutable in data. Re-creation of an
  existing Secret returns AlreadyExists and is skipped. ownerReference patching
  is idempotent — patching an already-set ownerReference is a no-op.
- **Refs are truly immutable**: Set at CER creation, never modified (inherited
  from phase immutability).

### CER reconciler behavior

When processing a CER phase:
- For each object entry in the phase:
  - If `object` is set, use it directly (current behavior, unchanged).
  - If `ref` is set, fetch the referenced Secret, read the value at the
    specified `key`, and JSON-deserialize into an
    `unstructured.Unstructured`.
- The resolved object is used identically to an inline object for the remainder
  of reconciliation — collision protection inheritance, owner labeling, and
  rollout semantics are unchanged.

Under normal operation, referenced Secrets are guaranteed to exist before the
CER is created (see [Crash-safe creation sequence](#crash-safe-creation-sequence)).
If a referenced Secret or key is not found — indicating an inconsistent state
caused by external modification or a partially completed creation sequence —
the reconciler sets a terminal error condition on the CER.

Secrets are fetched using the typed client served from the informer cache.

---

## Alternative: Externalized Phases to Secret Chains

All phases are serialized as a single JSON array, gzip-compressed into one blob,
and stored under a single `phases` data key in a Secret. If the compressed blob
exceeds ~900 KiB, it is split into fixed-size byte chunks across multiple
Secrets linked via `.nextSecretRef` keys.

### API change

Keep the existing `phases` field and add a new `phasesRef` field. Only one of the
two may be set, enforced via CEL validation. `phasesRef` points to the first
Secret in a chain of one or more Secrets containing the phase data.

```go
type PhasesRef struct {
    SecretRef SecretRef `json:"secretRef"`
}

type SecretRef struct {
    Name      string `json:"name"`
    Namespace string `json:"namespace"`
}
```

The reconciler follows `.nextSecretRef` data keys from Secret to Secret until a
Secret has no `.nextSecretRef`.

Both fields are immutable once set. `phases` and `phasesRef` are mutually
exclusive.

### Secret type and naming convention

Secrets use a dedicated type `olm.operatorframework.io/revision-phase-data` to
distinguish them from user-created Secrets and enable easy identification.

Secret names are derived deterministically from the CER name and a content hash.
The hash is the first 16 hex characters of the SHA-256 digest of the phases
serialized to JSON (before gzip compression). Computing the hash from the JSON
serialization rather than the gzip output makes it deterministic regardless of
gzip implementation details.

| Secret   | Name                          |
|----------|-------------------------------|
| First    | `<CER-name>-<hash>`           |
| Second   | `<CER-name>-<hash>-1`         |
| Third    | `<CER-name>-<hash>-2`         |
| Nth      | `<CER-name>-<hash>-<N-1>`     |

The hash is computed from the phases JSON before any Secrets are created, so
`phasesRef.secretRef.name` is known at CER creation time and can be set
immediately.

### Secret labeling

All Secrets in a chain carry a common label identifying the owning revision:

```
olm.operatorframework.io/revision-name: <CER-name>
```

This allows all phase data Secrets for a given revision to be fetched with a
single list call:

```sh
kubectl get secrets -l olm.operatorframework.io/revision-name=my-extension-1
```

This is useful for debugging, auditing, and bulk cleanup. It also provides an
efficient alternative to following the `.nextSecretRef` chain when all Secrets
need to be loaded at once.

### Secret structure

Each Secret has a single `phases` data key holding its chunk of the gzip blob.
If more Secrets follow, a `.nextSecretRef` key holds the name of the next Secret
in the chain. The dot prefix clearly distinguishes it from the `phases` key.

**Single Secret (all data fits in one):**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-extension-1-a1b2c3d4e5f67890
  labels:
    olm.operatorframework.io/revision-name: my-extension-1
  ownerReferences:
    - apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtensionRevision
      name: my-extension-1
      uid: <revision-uid>
      controller: true
immutable: true
type: olm.operatorframework.io/revision-phase-data
data:
  phases: <base64(gzip(JSON array of all phases))>
```

**Multiple Secrets (chunked):**

```yaml
# Secret my-extension-1-a1b2c3d4e5f67890 — chunk 0
apiVersion: v1
kind: Secret
metadata:
  name: my-extension-1-a1b2c3d4e5f67890
  labels:
    olm.operatorframework.io/revision-name: my-extension-1
  ownerReferences:
    - apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtensionRevision
      name: my-extension-1
      uid: <revision-uid>
      controller: true
immutable: true
type: olm.operatorframework.io/revision-phase-data
data:
  phases:         <base64(chunk 0 of gzip stream)>
  .nextSecretRef: <base64("my-extension-1-a1b2c3d4e5f67890-1")>
---
# Secret my-extension-1-a1b2c3d4e5f67890-1 — chunk 1
apiVersion: v1
kind: Secret
metadata:
  name: my-extension-1-a1b2c3d4e5f67890-1
  labels:
    olm.operatorframework.io/revision-name: my-extension-1
  ownerReferences:
    - apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtensionRevision
      name: my-extension-1
      uid: <revision-uid>
      controller: true
immutable: true
type: olm.operatorframework.io/revision-phase-data
data:
  phases:         <base64(chunk 1 of gzip stream)>
  .nextSecretRef: <base64("my-extension-1-a1b2c3d4e5f67890-2")>
---
# Secret my-extension-1-a1b2c3d4e5f67890-2 — chunk 2 (last)
apiVersion: v1
kind: Secret
metadata:
  name: my-extension-1-a1b2c3d4e5f67890-2
  labels:
    olm.operatorframework.io/revision-name: my-extension-1
  ownerReferences:
    - apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtensionRevision
      name: my-extension-1
      uid: <revision-uid>
      controller: true
immutable: true
type: olm.operatorframework.io/revision-phase-data
data:
  phases: <base64(chunk 2 of gzip stream)>
```

The last Secret has no `.nextSecretRef`. The reconciler follows the chain until
it encounters a Secret without `.nextSecretRef`.

### Compression

All phases are compressed together as a single gzip stream. Kubernetes manifests
are highly repetitive structured text and typically achieve 5-10x compression
with gzip. Compressing all phases together rather than individually exploits
cross-phase redundancy (shared labels, annotations, namespace references, etc.)
for better compression ratios.

This significantly reduces the number of Secrets needed and makes it likely that
most extensions fit in a single Secret. To inspect phase data:

```sh
# single Secret
kubectl get secret <name> -o jsonpath='{.data.phases}' | base64 -d | gunzip | jq .
```

This follows the same pattern used by Helm, which gzip-compresses release data
before storing in Secrets.

### Chunking strategy

The entire gzip blob is split at 900 KiB byte boundaries (leaving headroom for
base64 overhead and metadata). This is a simple byte-level split — phases are
never individually split because the chunking operates on the raw gzip stream,
not on individual phase boundaries.

If the total compressed size after chunking would require an unreasonable number
of Secrets, creation fails with a clear error.

### Crash-safe creation sequence

Because the hash is computed from the phases JSON before any Secrets are created,
`phasesRef` can be set at CER creation time:

```
Step 1: Create CER with phasesRef.secretRef.name = <CER-name>-<hash>
             |
             |  crash here → CER exists but Secrets do not.
             |                Reconciler retries Secret creation.
             v
Step 2: Create Secrets with ownerReferences pointing to the CER
             |
             |  crash here → Partial set of Secrets exists, all with
             |                ownerReferences. Not orphaned — GC'd if
             |                CER is deleted. Existing immutable Secrets
             |                are skipped (AlreadyExists), missing ones
             |                are created on next attempt.
             v
         Done — CER has phasesRef, all Secrets exist with owner refs.
```

Key properties:
- **No orphaned Secrets**: Every Secret carries an ownerReference to the CER.
  Kubernetes garbage collection removes them if the CER is deleted at any point.
- **Idempotent retry**: Secrets are immutable. Re-creation of an existing Secret
  returns AlreadyExists and is skipped. Missing Secrets are created on retry.
- **phasesRef is truly immutable**: Set at CER creation, never modified.

### CER reconciler behavior

When reconciling a CER:
- If `phases` is set, use it directly (current behavior, unchanged).
- If `phasesRef` is set:
  1. Fetch the Secret at `phasesRef.secretRef.name` in
     `phasesRef.secretRef.namespace`.
  2. Read `data.phases` (base64-decoded), append to a byte buffer.
  3. If `data[.nextSecretRef]` exists, fetch the named Secret and repeat from
     step 2.
  4. Gunzip the concatenated buffer.
  5. JSON-deserialize into `[]ClusterExtensionRevisionPhase`.
  6. Use identically to inline phases.
  If a Secret is not yet available, return a retryable error.
- If neither is set, skip reconciliation (invalid state).

The reconstructed phase list is used identically to inline phases for the
remainder of the reconciliation — ordering, collision protection inheritance, and
rollout semantics are unchanged.

---

## Comparison

| Dimension | Per-Object Content References                                                                         | Externalized Phases to Secret Chains                                                             |
|---|-------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------|
| **Granularity** | Per-object — each object has its own `ref`                                                            | Per-phase-set — all phases externalized as a single blob                                         |
| **API complexity** | New `ref` field on existing object struct; new `ObjectSourceRef` type                                 | New top-level `phasesRef` field; new `PhasesRef` and `SecretRef` types                           |
| **Reconciler complexity** | Secrets are served from cache; no ordering dependency between fetches                                 | Chain traversal — follow `.nextSecretRef` links, concatenate chunks, gunzip, deserialize         |
| **Compression** | Optional per-object gzip, auto-detected via magic bytes; each object compressed independently         | Single gzip stream across all phases; exploits cross-phase redundancy for better ratios          |
| **Number of Secrets** | One or more, typically one                                                                            | Typically one Secret for all phases; multiple only when compressed blob exceeds 900 KiB          |
| **Crash safety** | 3-step: Secrets → CER → patch ownerRefs; orphan cleanup via revision label                            | 2-step: CER → Secrets with ownerRefs; simpler but reconciler may see missing Secrets temporarily |
| **Flexibility** | Mixed inline/ref per object within the same phase is possible                                         | All-or-nothing — either all phases inline or all externalized                                    |
| **Storage efficiency** | Per-object compression misses cross-object redundancy; potentially more Secrets created in edge cases | Better compression from cross-phase redundancy; fewer Secrets                                    |
| **Resource type** | Secret only                                                                                          | Secret only (with dedicated type)                                                                |
| **Phases structure** | Unchanged — phases array preserved as-is; only individual objects gain a new resolution path          | Replaced at the top level — phases field swapped for phasesRef                                   |
| **Content addressability** | Content hash as Secret data key — key changes when content changes                                    | Content hash embedded in Secret name — detects changes without fetching contents                 |

---

## Non-goals

- **Migration of existing inline objects/phases**: Existing CERs using inline
  `object` or `phases` fields continue to work as-is. There is no automatic
  migration to `ref` or `phasesRef`.

- **System-managed lifecycle for system-created resources**: The OLM creates
  and owns the referenced Secrets it produces (setting ownerReferences and
  immutability). Users who manually craft CERs with `ref` are responsible for
  the lifecycle of their own referenced Secrets.

- **Cross-revision deduplication**: Objects or phases that are identical between
  successive revisions are not shared or deduplicated. Each revision gets its
  own set of referenced Secrets. Deduplication adds complexity with minimal
  storage benefit given that archived revisions are eventually deleted.

- **Lazy loading or streaming**: The reconciler loads all objects or phases into
  memory at the start of processing. Per-object streaming or lazy loading is not
  in scope.

- **Application-level encryption beyond Kubernetes defaults**: Referenced
  Secrets and phase data Secrets inherit whatever encryption-at-rest
  configuration is applied to Secrets at the cluster level. Application-level
  encryption of content is not in scope.

---

## Other Alternatives Considered

- **Increase etcd max-request-bytes**: Raises the per-object limit at the etcd
  level. This is an operational burden on cluster administrators, is not portable
  across clusters, degrades etcd performance for all workloads, and only shifts
  the ceiling rather than removing it.

- **Custom Resource for phase/object storage**: A dedicated CRD (e.g.,
  `ClusterExtensionRevisionPhaseData`) would provide schema validation and
  a typed API. However, it introduces a new CRD to manage, is subject to the
  same etcd size limit, and the phase data is opaque to the API server anyway
  (embedded `unstructured.Unstructured` objects). Secrets are simpler and
  sufficient.

- **External storage (OCI registry, S3, PVC)**: Eliminates Kubernetes size
  limits entirely but introduces external dependencies, availability concerns,
  authentication complexity, and a fundamentally different failure domain.
  Over-engineered for the expected data sizes (low single-digit MiBs).


## Recommendation

**Per-object content references** is the recommended approach for the following
reasons:

1. **Phases structure preserved**: The phases array in the CER spec remains
   unchanged. Only individual object entries gain a new resolution path via
   `ref`. This is a smaller, more targeted API change compared to replacing the
   entire phases field with `phasesRef`.

2. **Granular flexibility**: Inline `object` and `ref` can be mixed within the
   same phase. Users who manually craft CERs can choose either approach
   per-object. The externalized phases approach is all-or-nothing.

3. **Uniform code path**: By externalizing all objects by default, the system
   follows a single code path regardless of bundle size. There is no need for
   size-based heuristics to decide when to externalize.

The tradeoff is that per-object references create potentially more Secrets for very large bundles and miss
cross-object compression opportunities. In practice, this is acceptable: the
additional Secrets are small, immutable, garbage-collected via ownerReferences,
and the slight storage overhead is outweighed by the simpler reconciler logic
and greater flexibility.
