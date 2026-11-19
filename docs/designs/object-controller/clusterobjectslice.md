# ClusterObjectSlice: initial content-storage API

This proposal starts the [OPRUN-4850](https://redhat.atlassian.net/browse/OPRUN-4850)
discussion with an experimental, cluster-scoped API for storing rendered
Kubernetes manifests independently of rollout intent.

The API name in this proposal is **ClusterObjectSlice**, with short name `cosl`.

## Motivation and responsibilities

ClusterObjectDeployment (COD) describes rollout intent and creates
ClusterObjectSet (COS) revisions. COS applies their content in phases and checks
readiness. Today, inline manifests live inside rollout resources, while external
content is packed into immutable Secrets. Secret references separate storage from
rollout, but manifest identities and encoding remain a protocol layered over the
general-purpose Secret API.

A dedicated content API would expose that inventory in its schema, validate the
storage envelope at admission, and give manifest storage its own resource kind
for access control. It would also provide a common publication contract for
ClusterExtension and standalone publishers. Later integration could let several
rollout revisions reference the same stored content.

Keeping immutable Secrets remains a lower-cost alternative: it uses the existing
packing and resolution paths, requires no additional CRD, and preserves
namespace-based access boundaries. ClusterObjectSlice adds API maintenance and
cluster-scoped access decisions; its value depends on whether an explicit content
contract justifies those costs. A dedicated resource alone does not improve
payload readability, verify integrity, or coordinate retention.

The proposed responsibility split is:

| Component | Responsibility |
| --- | --- |
| Publisher | Prepare manifests and their declared identities, publish slices, and manage their lifetime. |
| ClusterObjectSlice API | Store immutable content and validate its envelope before persistence. |
| COD | Manage rollout intent and COS revisions. |
| COS | Apply content in phases, evaluate readiness, and manage ownership and teardown of live resources. |

Slice consumption by COD/COS is future integration work. This split keeps content
publication independent of rollout progress and resource readiness.

## Initial scope

The initial change introduces Go types, a generated CRD and apply configurations,
schema/CEL admission validation, API tests, and documentation. The CRD is installed
with the experimental object-controller APIs, including standalone installations.
Creating a slice stores content; it does not apply any objects.

ClusterObjectDeployment and ClusterObjectSet continue using their existing inline
and Secret-backed content. Producer changes, slice references, resolver changes,
webhooks, and retention controllers are separate follow-ups. This API has no
reconciler or status subresource.

This provides a concrete foundation for reviewing identity, encoding, validation,
and ownership. The experimental API can change as those discussions progress;
this proposal does not promise migration between experimental schema revisions.

## Initial representation and alternatives

The proposed first API stores encoded manifest bytes. This gives reviewers a
concrete storage envelope whose complete content can be made immutable with
schema/CEL validation, without introducing decoding infrastructure in this PR.

| Approach | Benefits | Costs and open work |
| --- | --- | --- |
| Encoded JSON or gzip-compressed JSON | One byte-oriented content field with explicit encoding; compression is available per object; native immutability covers the complete payload. | Reading and comparing manifests requires decoding. Declared identity duplicates information inside the payload, and schema/CEL cannot verify that they agree. Deep admission validation would require operating a webhook with bounded decoding. |
| Readable embedded manifests, with a compressed alternative | Uncompressed manifests can be inspected and compared directly; their contents are visible alongside the declared identity. | Supporting readable and compressed forms requires representation-selection rules. Preserving arbitrary manifest fields complicates complete immutability and admission checks; webhook enforcement may be needed. Compressed entries still require decoding and expansion limits. |

The common foundation is immutable storage, explicit target identity, and
separation from rollout intent. Encoded bytes are the initial representation
choice. Adding readable manifests may require revising the experimental payload
schema; this proposal leaves that change open.

## Initial API shape

| Field | Initial contract |
| --- | --- |
| `metadata.name` | Cluster-scoped name locating the slice. It is not a content digest. |
| `spec` | Immutable after creation; metadata remains mutable. |
| `spec.objects` | An unordered map list of 1–256 entries keyed by `apiVersion`, `kind`, `name`, and `namespace`. |
| `objects[].apiVersion` | Required API version or group/version, up to 317 characters. |
| `objects[].kind` | Required kind, 1–63 letters, digits, or hyphens; starts with a letter and ends with an alphanumeric character. |
| `objects[].name` | Required object name, 1–253 characters; cannot be `.`, `..`, or contain `/` or `%`. Target-specific naming checks happen during apply. |
| `objects[].namespace` | Explicit namespace for namespaced objects. Omission defaults to the empty string for cluster-scoped objects. Nonempty values must be DNS labels of at most 63 characters. |
| `objects[].encoding` | Required `JSON` or `GzipJSON`; encoding is explicit rather than detected from payload bytes. |
| `objects[].content` | Required bytes, base64-encoded on the wire. The encoded string is limited to 1–1,048,576 characters per entry. |

The 256-entry cap and per-entry encoded length are preliminary admission bounds.
They bound inventory size and individual encoded values; sizing evidence and
publisher packing requirements should inform their review. Aggregate serialized
size and gzip expansion need separate limits before consumers decode content.

One slice may store several objects, and a bundle may occupy several slices.
Storage grouping does not define a rollout revision, phase order, readiness
criteria, or ownership of applied resources. Object order within a slice has no
meaning; changes to list membership, identity fields, encoding, or content require
a new slice.

The declared target identity is an inventory and a potential lookup key. This
initial CRD cannot verify that the encoded manifest agrees with it. It also
cannot determine resource scope without target discovery: publishers must supply
the correct namespace. Two entries with different API versions can still describe
the same underlying resource.

### Example

The following slice stores a ConfigMap whose decoded manifest is:

```json
{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"example"},"data":{"message":"hello"}}
```

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterObjectSlice
metadata:
  name: example-content
spec:
  objects:
    - apiVersion: v1
      kind: ConfigMap
      name: settings
      namespace: example
      encoding: JSON
      content: eyJhcGlWZXJzaW9uIjoidjEiLCJraW5kIjoiQ29uZmlnTWFwIiwibWV0YWRhdGEiOnsibmFtZSI6InNldHRpbmdzIiwibmFtZXNwYWNlIjoiZXhhbXBsZSJ9LCJkYXRhIjp7Im1lc3NhZ2UiOiJoZWxsbyJ9fQ==
```

For `GzipJSON`, gzip-compress the JSON bytes before base64 encoding them. Both
representations describe one complete object, rather than a YAML document stream
or a Kubernetes `List`. Enforcing that payload contract is future validation work.

## What ingress validates

Ingress means API admission before persistence. The initial schema and CEL rules
validate required fields, identity formats, supported encoding values, entry
counts, per-entry encoded length, unique identity tuples, and immutable content.
These checks run without a content controller or admission webhook.

The initial API deliberately accepts opaque payload bytes. It does not parse
JSON, decompress gzip, compare the manifest with the identity header, or validate
a producer-supplied digest. Consequently, API acceptance does not establish that
the payload is a valid manifest or can be installed. Tests demonstrate this
boundary as well as the enforced checks.

Publication does not require target APIs to exist. A publisher can store a CRD
and instances of that CRD before either has been installed. Target-schema
validation, deployment authorization, and live-resource admission belong to the
appropriate rollout phase.

The per-entry limit bounds the base64 string, not gzip expansion or total slice
size. Publishers must also respect API-server request and storage limits and
split content accordingly. Limits on total encoded bytes, expanded bytes, and
decoding work need to be specified before introducing a resolver or webhook that
decodes slice content.

## Ownership and access

Publishers choose and maintain slice owner references and retention. A
cluster-scoped ClusterExtension can own a cluster-scoped slice; a namespaced
application cannot be its native owner. Standalone publishers must define an
appropriate cluster-scoped owner or explicitly manage deletion.

Native garbage collection follows owner references. A future COD/COS content
reference alone will not prevent deletion, and history pruning will not reclaim
content owned directly by a ClusterExtension. Any promise to retain content while
referenced needs a separate coordination design, especially when slices are
shared. This initial API introduces no reference count, finalizer, orphan sweep,
or automatic retention guarantee.

Dedicated publisher and reader roles can be designed when producers and consumers
are integrated. Reading or publishing content does not authorize deploying its
objects through a privileged consumer. Cluster scope also removes namespace-based
content delegation.

Encoded content can contain sensitive values. Base64 and gzip provide no
confidentiality, and a new CRD does not inherit Secret-specific encryption
configuration. Deployment guidance must cover resource access and encryption
policy before producers publish sensitive content.

## Decisions for this API PR

Before merging the initial API, reviewers should agree on these proposed choices:

1. Introduce a dedicated, cluster-scoped content resource, accepting its API
   maintenance and access-control costs compared with keeping immutable Secrets.
2. Make the complete spec immutable while permitting metadata updates. Publishing
   changed content requires a new slice.
3. Expose identity as `apiVersion`, `kind`, `name`, and `namespace`, and use that
   tuple as the unordered list key. The initial envelope cannot detect equivalent
   targets served through different API versions.
4. Start with explicit `JSON` and `GzipJSON` encodings and the proposed entry and
   encoded-length bounds. Keep readable manifests open for further API discussion.
5. Enforce envelope validation at admission and leave deep payload checks to a
   separate proposal. API acceptance alone does not establish payload validity.
6. Keep lifetime publisher-managed and introduce storage without a reconciler,
   status subresource, or COD/COS integration.

This PR establishes a baseline for those decisions. Expected digests, canonical
hashing, validation webhooks, and coordinated retention remain follow-up design
questions.

## Admission and storage scenarios

The following outcomes describe the initial API, assuming the caller is
authorized and the request satisfies ordinary API-server limits.

| Scenario | Expected observation |
| --- | --- |
| Publish a valid envelope | The slice is stored; creating it applies no managed objects. |
| Publish two entries with the same identity tuple | Admission rejects the duplicate list key. Omitted namespace and explicit empty namespace identify the same key. |
| Publish an empty list, more than 256 entries, or content exceeding its encoded-length bound | Admission rejects the envelope. |
| Change payload bytes, encoding, identity, or list membership | Admission rejects the immutable spec update; the stored content remains unchanged. |
| Reorder existing entries without changing their values | The update is accepted; entry order has no rollout meaning. |
| Update labels, annotations, owner references, or finalizers while preserving the spec | Slice content validation permits the metadata update; normal metadata validation and authorization still apply. |
| Publish malformed JSON/gzip bytes or a payload that disagrees with its identity header | Envelope validation can accept the slice. Deep payload validity is outside the initial admission contract. |
| Publish content for an API whose CRD is not installed | Publication can succeed; target discovery and schema validation are deferred to consumption and apply. |

## Decisions left open for incremental work

### Representation and deeper admission validation

Representation and validation depth need separate decisions. A follow-up can
retain encoded entries and add payload validation, or revisit readable manifests
and define validation for both readable and compressed forms. Arbitrary preserved
fields are not accessible to
[CRD CEL validation](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#validation-rules).
Complete immutability for readable manifests must therefore be demonstrated,
with a webhook if necessary.

A webhook enforcing required checks needs an explicit failure policy, bounded
decoding and concurrency, serving certificates, dry-run behavior, and outage
tests. Its matching must account for metadata/finalizer updates and exclude
unnecessary operations. Adding stricter validation also requires deciding how
previously accepted payloads are handled.

### Consumption and integrity

A follow-up must define COD/COS reference fields, missing-content behavior, and
producer-supplied expected digests. A reference by name and identity alone cannot
detect replacement after deletion. Canonical hashing rules should give JSON and
gzip representations of the same manifest the same logical content digest.

The proposed consumer requirements are to verify expected digests before apply,
enforce decoding limits, compare headers with decoded identities, and detect
duplicate targets across referenced and inline entries, phases, and equivalent
API versions. The consumer proposal must settle those requirements and their
failure behavior. Publication validation cannot replace checks of the content
actually consumed.

Teardown should use recorded managed-object identities so missing slice content
does not prevent cleanup.

### Lifecycle and preparation

Producer-owned lifetime is the initial contract. Retention while referenced,
publication reservations, consumer pins, and coordinated retirement need a
separate proposal before automatic collection is introduced.

A content reconciler is justified only by concrete preparation or lifecycle
work beyond ingress validation. Storage alone requires neither a reconciler nor
an `ArtifactReady` condition.

## References

- [Orb's ClusterObjectSlice API](https://github.com/joelanford/orb-operator/blob/76c7c2316efdacf9a3eeb8a68719df9781dacf1e/api/v1alpha1/types_clusterobjectslice.go)
- [OpenShift API conventions](https://github.com/openshift/enhancements/blob/master/dev-guide/api-conventions.md)
- [Kubernetes owner references](https://kubernetes.io/docs/concepts/architecture/garbage-collection/#owners-and-dependents)
- [Kubernetes encryption at rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
