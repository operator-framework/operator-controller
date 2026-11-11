# ClusterObjectDeployments

A `ClusterObjectDeployment` (COD) is an experimental, cluster-scoped API that
manages mutable desired content through immutable
[ClusterObjectSet](clusterobjectsets.md) (COS) revisions. Update the deployment's
template to change what should be deployed; the controller creates a new revision
rather than modifying an existing revision's content.

For a hands-on walkthrough, see
[Manage standalone ClusterObjectDeployments](../tutorials/manage-clusterobjectdeployments.md).
For field definitions, see the
[COD API reference](../../api-reference/olmv1-api-reference.md#clusterobjectdeployment).

## Responsibilities

COD and COS separate rollout orchestration from resource reconciliation:

| ClusterObjectDeployment | ClusterObjectSet |
| --- | --- |
| Stores the mutable desired template | Records the content of one revision |
| Creates and owns revisions | Applies objects in ordered phases |
| Enforces rollout deadlines | Evaluates readiness and records first completion |
| Reports deployment availability and progress | Reports resource reconciliation through `Ready` |
| Prunes excess archived history | Handles object ownership handoff and teardown |

Both APIs are reconciled by the experimental object-controller. They can be used
without ClusterExtension or ClusterCatalog resources. See
[Controller deployment](clusterobjectsets.md#controller-deployment) for standalone
installation details.

## Desired content and rollout policy

A COD has three main spec fields:

- `spec.template` contains the desired revision content and revision metadata.
- `spec.progressDeadlineMinutes` configures the rollout budget independently of
  revision content.
- `spec.revisionHistoryLimit` controls how much archived history to retain.

For example, this deployment allows ten minutes for a revision to roll out and
retains two archived revisions:

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterObjectDeployment
metadata:
  name: my-app
spec:
  progressDeadlineMinutes: 10
  revisionHistoryLimit: 2
  template:
    metadata:
      labels:
        app.kubernetes.io/name: my-app
    spec:
      collisionProtection: Prevent
      phases:
      - name: configuration
        objects:
        - type: InlineManifest
          inlineManifest:
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: my-app-settings
              namespace: default
            data:
              message: Hello from revision A
```

The template's `spec` contains phases, optional custom progression probes, and
collision protection, with the same semantics as the corresponding COS fields.
Objects can be embedded as `inlineManifest` or stored in immutable Secrets and
referenced with `secretRef`. The controller assigns each revision's group,
revision number, lifecycle state, and controller owner reference; callers cannot
set these through the template.

`spec.template.metadata` contains labels and annotations propagated to each COS,
not to its managed objects. Set a managed object's metadata in its own manifest.
The reserved `olm.operatorframework.io/template-hash` label is always set by the
controller to identify the template that produced a revision.

## Revision lifecycle

The COD's name identifies its COS group. Names must be 1–52 characters long,
start with a lowercase letter, contain only lowercase letters, digits or hyphens,
and end with a letter or digit.

When a new revision is needed, the controller:

1. Finds the highest revision number in the group, including archived and
   foreign-owned revisions.
2. Allocates the next number (`1` for an empty group) and creates an owned, Active
   COS named `<deployment-name>-<revision>`.
3. Observes that revision's rollout while older revisions can remain Active.
4. Reports completion once the new COS records `status.completedAt`.

The COS controller handles ownership handoff between revisions and archives
superseded revisions once handoff completes. The COD controller prunes excess
archived history after teardown finishes.

Revision numbers range from 1 to 9999999999. Once that range is exhausted, the
controller reports an error rather than allocating another revision.

### Which edits create revisions?

The controller compares the desired template with the latest owned, active,
non-deleting revision's template identity. A different template, or the absence
of an active revision, causes a new revision to be created, subject to the
[ownership rules](#revision-ownership).

| Edit | Result |
| --- | --- |
| Objects, Secret references, phases, probes, or collision protection in `spec.template.spec` | A new revision |
| Phase, object, or probe order | A new revision |
| Labels or annotations in `spec.template.metadata` | A new revision |
| `spec.progressDeadlineMinutes` | The existing revision is retained; its rollout budget changes |
| `spec.revisionHistoryLimit` | Retention changes; the existing revision is retained |
| Root COD labels or annotations | The existing revision is retained |

Reapplying an unchanged template retains the current active revision. Restoring
an earlier template after a different one has been deployed creates a new,
higher-numbered revision; it does not reactivate archived history. For example,
A → B → A produces revisions 1, 2, and 3.

### Revision ownership

Group membership determines revision ordering; controller owner references
determine which revisions a COD manages. Sharing a group does not by itself make
a COS owned by the deployment.

The controller can adopt an ownerless, non-deleting group member only when it
carries the `apps.kubernetes.io/managed-by=object-controller` label. It does not
replace another controller's owner reference. A stale reference to an older UID
of the same deployment is left for Kubernetes garbage collection rather than
rewritten.

If the highest active, non-deleting group member is controlled by another
controller, the COD reports `Progressing=False/OwnerConflict` and creates no new
revision. If that group member is ownerless but lacks the managed-by label, the
COD reports `Progressing=False/UnmanagedRevision`. Resolve the foreign ownership,
remove the blocking revision, or restore a missing managed-by label only when the
revision was actually created by object-controller. A stale owner reference to an
older UID also blocks new revisions until garbage collection removes it, without
a distinct condition reason.

`OwnerConflict` concerns ownership of the COS itself; `RevisionObjectCollision`
concerns ownership of Kubernetes objects within a revision. Ownership checks take
precedence over rollout completion and deadline checks. Availability still
reflects only owned active revisions, so an older ready revision can keep the COD
available during a conflict.

## Status conditions

COD exposes `Available` and `Progressing`. Its child COSs expose `Ready` and
record first rollout completion in `status.completedAt`.

### Available

Availability describes current health across owned, active, non-deleting
revisions, not just the latest revision:

| Status | Reason | Meaning |
| --- | --- | --- |
| True | `Available` | At least one owned active revision is Ready |
| False | `NoActiveRevision` | No owned active revision exists |
| False | `Unavailable` | Owned active revisions exist, but none are Ready |

An older ready revision can keep `Available=True` while its successor rolls out,
is stalled, or has exceeded its rollout deadline.

### Progressing

Progress describes the desired rollout, including failures that prevent it from
starting or completing:

| Status | Reason | Meaning |
| --- | --- | --- |
| True | `NewClusterObjectSetProgressing` | Waiting to create or adopt revisions, or the desired revision is rolling out |
| True | `RevisionPreconditionsNotMet` | The desired revision's preconditions are unmet; reconciliation keeps retrying |
| False | `NewClusterObjectSetProgressed` | The desired revision completed rollout at least once |
| False | `ProgressDeadlineExceeded` | An incomplete, still-retrying revision exceeded its rollout budget |
| False | `InvalidSpecification` | Template labels or annotations are invalid; no revision is created until corrected |
| False | `OwnerConflict` | The highest active group member is controlled by another controller |
| False | `UnmanagedRevision` | The highest active group member is ownerless and lacks the object-controller managed-by label |
| False | `RevisionObjectCollision` | The desired revision cannot take ownership of one or more managed objects |
| False | `RevisionInvalidSpecification` | The desired revision's rendered objects are invalid |

For revision-specific failures, the COD's condition message names the COS to
inspect. Its `Ready` condition provides the specific cause.
`RevisionInvalidSpecification` concerns the revision's rendered objects, while
`InvalidSpecification` concerns the COD's `spec.template.metadata`.

`RevisionObjectCollision` and `RevisionInvalidSpecification` are hard stalls:
they require user action and take precedence over `ProgressDeadlineExceeded`.
`RevisionPreconditionsNotMet` is a retrying state and remains subject to the
rollout deadline.

Completion is historical. Once the desired COS records `status.completedAt`,
later health regressions can affect `Available` but do not restart that
revision's rollout or its deadline. A new template starts a new rollout.

## Progress deadlines and recovery

`spec.progressDeadlineMinutes` supplies one budget for the whole revision. The
supported range is 1–720 minutes. Omit the field to disable deadline enforcement.
The deadline belongs to the COD; COSs do not carry a deadline field.

Timing starts at the COS's creation timestamp. Failed or delayed creation
attempts consume no budget. Completing phases, polling, retries, and controller
restarts do not reset the timer. A five-phase revision with a ten-minute deadline
has ten minutes total, not ten minutes per phase. Each new revision receives a
fresh budget.

Changing the deadline does not create a revision or reset its start time. The new
budget applies to the latest active revision and is still measured from that
COS's original creation timestamp:

- Extending the budget beyond elapsed rollout time can clear an existing
  `ProgressDeadlineExceeded` condition without new rollout progress.
- Shortening the budget below elapsed rollout time can cause immediate expiry.
- Removing the field disables enforcement, including after expiry.

The COD schedules its own expiry check even if the COS emits no further events.
When an incomplete, retrying revision exceeds the budget, the COD reports
`Progressing=False/ProgressDeadlineExceeded`. The COS's `Ready` condition retains
its resource reconciliation status; for example, it can still report
`PreconditionsNotMet`. Hard stalls and revision ownership conflicts retain their
more actionable condition reasons rather than being replaced by a timeout.

Reconciliation continues after expiry. A revision that later completes records
`status.completedAt`, and the COD reports
`Progressing=False/NewClusterObjectSetProgressed`. Completing only some phases
does not clear an expired deadline. Once completion is recorded, the deadline no
longer applies to that revision, even if it later becomes unhealthy.

## Revision history and deletion

`spec.revisionHistoryLimit` controls how many archived revisions the COD retains.
The default is 5, the supported range is 0–50, and 0 retains no archived history.
Active revisions never count against the limit and are not pruned by it.

Only owned archived revisions that have finished teardown are eligible for
pruning. The controller deletes the oldest eligible revisions beyond the limit;
revisions still tearing down are left until cleanup completes. Foreign-owned
group members do not count toward the limit and are not pruned by the COD.

Deleting a COD normally cascades to its owned COSs and their managed objects.
Deleting with orphan propagation leaves the revisions in their existing group;
it does not change revision membership or ownership handoff behavior.

## Relationship with ClusterExtension

When `BoxcutterRuntime` is enabled, operator-controller resolves and unpacks a
ClusterExtension's bundle and applies a COD with the same name. The
ClusterExtension owns the COD, the COD owns its COS revisions, and each COS owns
the Kubernetes objects it manages.

The COD's name also becomes the COS group. Revision lookup uses that group, not
labels or controller owner references. Operator-controller observes COD rollout
status to report the extension's installation and progress.

For a ClusterExtension, COD `OwnerConflict` maps to `Progressing=True` with reason
`Retrying`, preserving the deployment's message about the conflicting revision
and its controller. A first install reports `Installed=False` with reason
`Failed`. During an upgrade, the previously installed bundle and its
`Installed=True` condition are preserved until the new rollout completes, with
`observedGeneration` updated to acknowledge the current spec. Reconciliation
resumes automatically when the ownership conflict clears.
