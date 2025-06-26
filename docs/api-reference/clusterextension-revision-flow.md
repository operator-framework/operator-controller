# ClusterExtension to ClusterExtensionRevision Flow

This document describes the interaction flow between `ClusterExtension` and `ClusterExtensionRevision` resources in OLM v1, explaining how upgrade notifications and approvals work.

## Overview

The `ClusterExtensionRevision` feature provides a mechanism for:
- **Detecting available upgrades** for installed ClusterExtensions
- **Notifying users** when upgrades become available
- **Controlling upgrade timing and policy** through an approval workflow 
- **Preventing automatic upgrades** by requiring explicit approval for version changes
- **Preserving user's version constraints** without overwriting them during upgrades
- **Respecting version constraints** for upgrade detection

## Architecture

### Key Components

1. **ClusterExtension**: Represents an installed operator/extension with upgrade approval logic
2. **ClusterExtensionRevision**: Represents an available upgrade for a ClusterExtension
3. **ClusterExtensionRevision Controller**: Monitors catalogs and creates/manages revision resources
4. **ClusterExtension Controller**: Enhanced to check for approved revisions before performing upgrades
5. **Catalog Resolver**: Determines available upgrades using existing resolution logic

### Design Constraints

- **One-to-One Relationship**: Each ClusterExtension can have at most one ClusterExtensionRevision
- **Latest Upgrade Only**: Only the latest available upgrade is tracked as a revision
- **Approval-Based**: Upgrades only occur when explicitly approved by users
- **Initial Install Exception**: Initial installations proceed without approval (no existing version)
- **Version Constraint Preservation**: Original version constraints are never overwritten
- **Version Constraint Aware**: Respects version constraints for upgrade detection

### Version Constraint Handling

The controller handles three scenarios for version constraints:

1. **No Version Constraint**: Finds any available upgrade
2. **Pinned Version** (exact version): No ClusterExtensionRevision created (no upgrades allowed)
3. **Version Range**: Finds upgrades within the specified range

## Flow Diagram

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────────┐
│   ClusterCatalog │    │ ClusterExtension │    │ClusterExtensionRev. │
│                 │    │                  │    │                     │
│  [Catalog Data] │    │   [Installed]    │    │   [Upgrade Avail.]  │
└─────────────────┘    └──────────────────┘    └─────────────────────┘
         │                        │                         │
         │ 1. Catalog Update      │                         │
         ├────────────────────────┼─────────────────────────┤
         │                        │                         │
         │        2. Controller Detects Change              │
         │           ┌─────────────▼─────────────┐           │
         │           │  Revision Controller      │           │
         │           │  - Monitors catalogs      │           │
         │           │  - Checks for upgrades    │           │
         │           │  - Handles version ranges │           │
         │           │  - Manages revisions      │           │
         │           └─────────────┬─────────────┘           │
         │                        │                         │
         │        3. Version Constraint Analysis            │
         │           ┌─────────────▼─────────────┐           │
         │           │ Check Version Constraint  │           │
         │           │ - Pinned? Skip revision   │           │
         │           │ - Range? Find in range    │           │
         │           │ - None? Find any upgrade  │           │
         │           └─────────────┬─────────────┘           │
         │                        │                         │
         │        4. Find Available Upgrade                 │
         │           ┌─────────────▼─────────────┐           │
         │           │   Catalog Resolver        │           │
         │           │  - Query available vers.  │           │
         │           │  - Respect constraints    │           │
         │           │  - Compare with installed │           │
         │           │  - Return upgrade info    │           │
         │           └─────────────┬─────────────┘           │
         │                        │                         │
         │        5. Create/Update Revision                 │
         │                        ├─────────────────────────▶
         │                        │         6. User/Policy controller Reviews and Approves
         │                        │                         │
         │        7. User/Policy controller Approves Revision (approved=true)│
         │                        │                         │
         │        8. ClusterExtension Controller Detects    │
         │           Upgrade and Checks for Approval        │
         │                        │                         │
         │        9. Upgrade Executed (if approved)         │
         │                        │                         │
```

## Detailed Flow

### 1. Catalog Change Detection

**Trigger**: ClusterCatalog image reference changes (new catalog content available)

**Process**:
- ClusterCatalog controller polls for catalog updates (if polling enabled)
- When new catalog content is detected, ClusterExtensionRevision controller is notified
- Controller reconciles all ClusterExtensions to check for available upgrades

### 2. Upgrade Detection with Version Constraints

**Logic for version constraint handling**:

```go
// upgrade detection logic
func findAvailableUpgrade(ext *ClusterExtension, installedBundle *BundleMetadata) (*AvailableUpgrade, error) {
    // If no version constraint is specified, find any available upgrade
    if ext.Spec.Source.Catalog == nil || ext.Spec.Source.Catalog.Version == "" {
        return findAnyAvailableUpgrade(ctx, ext, installedBundle)
    }

    versionConstraint := ext.Spec.Source.Catalog.Version

    // Check if this is a pinned version (exact version match)
    if isPinnedVersion(versionConstraint, installedBundle.Version) {
        // No upgrades for pinned versions - no revision created
        return nil, nil
    }

    // For version ranges, find upgrades within the range
    return findUpgradeInVersionRange(ctx, ext, installedBundle)
}

// Check if version is pinned (exact match without operators)
func isPinnedVersion(versionConstraint, installedVersion string) bool {
    // Consider it pinned if:
    // 1. No operators like >=, <=, >, <, ~, ^, ||
    // 2. Exact match with installed version
    hasOperators := strings.ContainsAny(versionConstraint, "><=~^|")
    if hasOperators {
        return false
    }
    return strings.TrimSpace(versionConstraint) == strings.TrimSpace(installedVersion)
}
```

### Version Constraint Scenarios

#### Scenario 1: No Version Constraint
```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
spec:
  source:
    catalog:
      packageName: my-operator
      # No version constraint
```
**Behavior**: Finds any available upgrade, removing version constraints during resolution.

#### Scenario 2: Pinned Version
```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
spec:
  source:
    catalog:
      packageName: my-operator
      version: "1.2.3"  # Exact version
```
**Behavior**: No ClusterExtensionRevision created - version is pinned.

#### Scenario 3: Version Range
```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
spec:
  source:
    catalog:
      packageName: my-operator
      version: ">=1.2.0, <2.0.0"  # Version range
```
**Behavior**: Finds upgrades within the specified range, respecting the constraint.

### 3. ClusterExtensionRevision Management

**Creation**: When an upgrade is available and no revision exists
```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtensionRevision
metadata:
  name: my-extension-1.2.0
  ownerReferences:
  - apiVersion: olm.operatorframework.io/v1
    kind: ClusterExtension
    name: my-extension
spec:
  clusterExtensionRef:
    name: my-extension
  version: "1.2.0"
  bundleMetadata:
    name: my-extension
    version: "1.2.0"
  availableSince: "2024-01-15T10:30:00Z"
  approved: false  # Defaults to false
```

**Update**: When a newer upgrade becomes available
- The existing revision is updated to reflect the latest available upgrade
- `availableSince` timestamp is updated
- `approved` field is reset to `false`

**Cleanup**: When no upgrades are available
- Obsolete revisions are deleted
- This happens when:
  - The installed version is already the latest
  - Version is pinned (exact match)
  - No upgrades exist within the version range

**No Creation**: When version is pinned
- No ClusterExtensionRevision is created for pinned versions
- Pinned versions are detected by exact version match without operators

### 4. Approval Workflow

**User Action**: Set `approved: true` on the ClusterExtensionRevision

```yaml
# User approves the upgrade
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtensionRevision
metadata:
  name: my-extension-1.2.0
spec:
  # ... other fields
  approved: true  # User sets this to approve
```

**Controller Response**: 
- Watches for `approved` field changes from `false` to `true`
- Sets approval timestamp on the revision
- ClusterExtension controller detects approved revisions during reconciliation

### 5. Upgrade Execution

**Enhanced ClusterExtension Controller Process**:
```go
func (r *ClusterExtensionReconciler) reconcile(ctx context.Context, ext *ClusterExtension) error {
    // ... existing resolution logic ...
    
    // NEW: Check if this is an upgrade that requires approval
    if installedBundle != nil && installedBundle.Version != resolvedBundleVersion.String() {
        // This is an upgrade - check for approved revision
        if approved, err := r.isUpgradeApproved(ctx, ext, resolvedBundleVersion.String()); err != nil {
            return err
        } else if !approved {
            // No approved revision found, don't upgrade
            log.Info("upgrade available but not approved, waiting for approval")
            return nil // Don't proceed with upgrade
        }
        // Upgrade is approved, continue with installation
    }
    
    // ... continue with normal installation/upgrade process ...
}

func (r *ClusterExtensionReconciler) isUpgradeApproved(ctx context.Context, ext *ClusterExtension, targetVersion string) (bool, error) {
    // List all revisions for this ClusterExtension
    var revisions ClusterExtensionRevisionList
    if err := r.List(ctx, &revisions); err != nil {
        return false, err
    }
    
    // Check for approved revision with target version
    for _, revision := range revisions.Items {
        if revision.Spec.ClusterExtensionRef.Name == ext.Name &&
           revision.Spec.Version == targetVersion &&
           revision.Spec.Approved {
            return true, nil
        }
    }
    return false, nil
}
```

**ClusterExtensionRevision Controller Process**:
```go
func (r *ClusterExtensionRevisionReconciler) upgradeClusterExtension(ctx context.Context, ext *ClusterExtension, revision *ClusterExtensionRevision) error {
    // Check if upgrade is already completed
    if ext.Status.Install != nil && ext.Status.Install.Bundle.Version == revision.Spec.Version {
        // Upgrade completed, clean up the revision
        return r.Delete(ctx, revision)
    }
    
    // The approved revision exists - ClusterExtension controller will handle the upgrade
    return nil
}
```

**Result**: 
- **Initial installations**: Proceed normally without approval checks
- **Upgrades**: Only proceed if there's an approved ClusterExtensionRevision
- **Version constraints**: Original user constraints are preserved (never overwritten)
- **Cleanup**: ClusterExtensionRevision is deleted after successful upgrade

## State Transitions

### ClusterExtensionRevision Lifecycle

```
[Extension Installed] 
         │
         │ Check Version Constraint
         ├─────────────────────────┬─────────────────────────┐
         │                         │                         │
         │ Pinned Version          │ Version Range           │ No Constraint
         ▼                         ▼                         ▼
[No Revision Created]       [Check Range for Upgrades] [Find Any Upgrade]
                                   │                         │
                                   │ Upgrade Available       │ Upgrade Available
                                   ▼                         ▼
                            [Revision Created: approved=false]
                                   │                         │
                                   │ User Approval           │
                                   ▼                         ▼
                            [Revision Approved: approved=true]
                                   │                         │
                                   │ ClusterExtension Reconcile │
                                   ▼                         ▼
                            [Approval Check: isUpgradeApproved()]
                                   │                         │
                                   │ Upgrade Executed        │
                                   ▼                         ▼
                            [Extension Status Updated]
                                   │                         │
                                   │ Revision Cleanup        │
                                   ▼                         ▼
                            [Revision Deleted - Upgrade Complete]
```

### ClusterExtension Integration

The ClusterExtensionRevision controller integrates with ClusterExtension lifecycle:

- **Installation**: No revisions created until extension is successfully installed
- **Initial Install**: ClusterExtension controller proceeds without approval checks
- **Pinned Version**: No revisions created for exact version matches
- **Version Range**: Revisions created only for upgrades within the range
- **Upgrade Available**: Revision created with `approved=false`
- **User Approval**: User sets `approved=true`
- **Upgrade Check**: ClusterExtension controller checks for approved revisions during reconcile
- **Upgrade Execution**: Only proceeds if approved revision exists for target version
- **Version Preservation**: Original version constraints are never modified
- **Cleanup**: Revision deleted after successful upgrade completion

## Controller Behavior

### Reconciliation Triggers

The ClusterExtensionRevision controller reconciles when:

1. **ClusterCatalog changes**: New catalog content may contain upgrades
2. **ClusterExtension changes**: Installation status or spec changes  
3. **ClusterExtensionRevision changes**: Approval status changes

The ClusterExtension controller reconciles when:

1. **ClusterExtension changes**: Spec or metadata changes
2. **ClusterCatalog changes**: New catalog content may affect resolution
3. **Normal reconcile loop**: Periodic reconciliation (with approval checks for upgrades)

### Enhanced Controller Logic

```go
func (r *ClusterExtensionRevisionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Get the ClusterExtension
    ext := &ocv1.ClusterExtension{}
    if err := r.Get(ctx, req.NamespacedName, ext); err != nil {
        if apierrors.IsNotFound(err) {
            // Extension deleted - cleanup revisions
            return r.cleanupRevisionsForDeletedExtension(ctx, req.Name)
        }
        return ctrl.Result{}, err
    }

    // Handle approved revisions (upgrade flow)
    if err := r.handleApprovedRevision(ctx, ext); err != nil {
        return ctrl.Result{}, err
    }

    // Check for available upgrades and manage revisions
    // This now includes version constraint handling
    return ctrl.Result{RequeueAfter: 30 * time.Minute}, r.reconcileExtensionRevisions(ctx, ext)
}
```

**Enhanced ClusterExtension Controller Logic**:
```go
func (r *ClusterExtensionReconciler) reconcile(ctx context.Context, ext *ClusterExtension) (ctrl.Result, error) {
    // ... existing setup and installed bundle detection ...
    
    // Run resolution to find available bundles
    resolvedBundle, resolvedVersion, _, err := r.Resolver.Resolve(ctx, ext, installedBundle)
    if err != nil {
        return ctrl.Result{}, err
    }
    
    // NEW: Check if this is an upgrade that requires approval
    if installedBundle != nil && installedBundle.Version != resolvedVersion.String() {
        // This is an upgrade - check for approved revision
        if approved, err := r.isUpgradeApproved(ctx, ext, resolvedVersion.String()); err != nil {
            return ctrl.Result{}, err
        } else if !approved {
            // No approved revision found, don't upgrade - wait and requeue
            log.Info("upgrade available but not approved, waiting for approval")
            setInstalledStatusFromBundle(ext, installedBundle)
            setStatusProgressing(ext, nil) // No error, just waiting
            return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
        }
        // Upgrade is approved, continue with installation
        log.Info("upgrade approved, proceeding with installation")
    }
    
    // Continue with normal installation/upgrade process
    // ... existing installation logic unchanged ...
}
```

## User Experience

### Understanding Version Constraints

Users should understand how version constraints affect upgrade detection:

**Pinned Versions**: No upgrades will be detected
```yaml
# No ClusterExtensionRevision will be created
spec:
  source:
    catalog:
      version: "1.2.3"
```

**Version Ranges**: Upgrades detected within range
```yaml
# ClusterExtensionRevision created for upgrades in range
spec:
  source:
    catalog:
      version: ">=1.2.0, <2.0.0"
```

**No Constraints**: Any upgrade detected
```yaml
# ClusterExtensionRevision created for any newer version
spec:
  source:
    catalog:
      packageName: my-operator
      # No version constraint
```

### Discovering Available Upgrades

Users can discover available upgrades by listing ClusterExtensionRevisions:

```bash
# List all available upgrades
kubectl get clusterextensionrevisions

# Check specific extension
kubectl get clusterextensionrevisions -l clusterextension=my-extension
```

### Approving Upgrades

Users approve upgrades by patching the revision:

```bash
# Approve an upgrade
kubectl patch clusterextensionrevision my-extension-1.2.0 \
  --type='merge' -p='{"spec":{"approved":true}}'
```

### Monitoring Upgrade Status

Users can monitor the upgrade process through:

1. **ClusterExtensionRevision status**: Track approval and upgrade initiation
2. **ClusterExtension status**: Monitor actual upgrade progress
3. **Events**: Kubernetes events provide detailed upgrade information

## Benefits

1. **Proactive Notifications**: Users are notified when upgrades become available
2. **Controlled Upgrades**: Users decide when to apply upgrades through explicit approval
3. **Automatic Upgrade Prevention**: No accidental upgrades - all version changes require approval
4. **Version Constraint Preservation**: Original user constraints are never overwritten
5. **Initial Install Flow**: New installations proceed normally without approval workflow
6. **Version Constraint Awareness**: Respects existing version constraints for upgrade detection
7. **Pinned Version Support**: No unwanted upgrade notifications for pinned versions
8. **Range-Based Upgrades**: Finds upgrades within specified version ranges
9. **Clean Separation**: ClusterExtensionRevision manages detection, ClusterExtension manages execution
10. **Integration**: Leverages existing ClusterExtension upgrade mechanisms
11. **Simplicity**: One revision per extension, direct approval checks

## Limitations

1. **Latest Only**: Only tracks the latest available upgrade within constraints
2. **Single Approval**: No support for bulk approval of multiple extensions
3. **No Rollback**: Revisions don't support rollback to previous versions
4. **Manual Process**: Approval is manual; no automatic upgrade policies yet
5. **Pinned Versions**: No upgrade path for pinned versions
6. **Initial Install Bypass**: Initial installations skip the approval workflow (by design)
7. **Requeue Frequency**: Upgrade checks happen every 5 seconds when waiting for approval

## Future Work
