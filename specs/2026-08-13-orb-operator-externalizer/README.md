---
status: in-progress
---
# orb-operator Externalizer

## Summary

Implement an `Externalize` function that takes a COD apply configuration with inline objects and, if the serialized COD would exceed etcd's size limit, rewrites it to use `objectRef` entries pointing to ClusterObjectSlice (COSL) resources. When the COD is small enough, it is returned unchanged.

## Design

### Problem

etcd enforces a ~1.5 MiB limit per resource. A COD with many or large inline objects can exceed this. The apiserver also adds overhead fields on creation (uid, creationTimestamp, generation, managedFields, status subresource) that aren't present in the apply configuration but count toward the etcd limit. We need to estimate this overhead and externalize when the combined size would be too large.

### Approach: All-or-Nothing Externalization

When the estimated serialized size of a COD exceeds the safe threshold (900 KiB, matching `SecretPacker`'s conservative budget), **all** inline objects are externalized into COSLs. No partial externalization - this keeps the logic simple and predictable.

### Single Entry Point

The package exposes one function in `internal/operator-controller/applier/orb/externalizer.go`:

```go
func Externalize(
    cod *orbac.ClusterObjectDeploymentApplyConfiguration,
) (*orbac.ClusterObjectDeploymentApplyConfiguration, []*orbac.ClusterObjectSliceApplyConfiguration, error)
```

**Behavior**:
1. Serialize the COD to JSON and check whether it exceeds `maxDataSize` (900 KiB).
2. If under the limit, return the COD unchanged with a nil slice list.
3. If over the limit, pack all inline objects into COSLs, rewrite the COD's phase objects to use `objectRef` entries, and return the modified COD plus the COSL apply configurations.

**Callers** create the COSLs before applying the COD. The function does not touch the cluster.

### COSL Structure

Each COSL holds up to 256 `SliceObject` entries (the API maximum). Each `SliceObject` has:
- `apiVersion`, `kind`, `name`, `namespace` - extracted from the inline object's raw JSON
- `content` - the raw JSON bytes, always gzip-compressed

The COSL's serialized size must also stay under etcd's limit. We use the same 900 KiB conservative budget for each COSL's combined content (measured after compression). When a COSL would exceed this, we finalize it and start a new one.

### COSL Naming

Content-addressable: `<cod-name>-<hash>` where hash is the first 16 hex characters of the SHA-256 digest of the sorted concatenated content. This mirrors `SecretPacker`'s naming scheme and means identical content won't create duplicate COSLs across reconciles. The COD name is extracted from the COD apply configuration's metadata.

### PhaseObject Transformation

When replacing inline objects with refs:
- `Object` is set to nil
- `ObjectRef` is set to point to the COSL name and the object's identity (apiVersion, kind, name, namespace)
- `CollisionProtection` and `Assertions` are preserved as-is (they apply identically for inline and ref objects)

### Gzip Compression

All `SliceObject.Content` entries are gzip-compressed unconditionally. The COSL API auto-detects gzip format by checking the magic number in the first two bytes.
