# Implementation Plan

1. Create the `internal/operator-controller/applier/orb/` package with `externalizer.go`
   - Implement `Externalize(cod) (cod, cosls, error)`:
     - Serialize COD to JSON, compare against `maxDataSize`
     - If under limit, return unchanged
     - If over limit, iterate phases and objects, extract identity and content from each inline object
     - Build SliceObject entries with identity fields and (optionally gzip-compressed) content
     - Bin-pack into COSLs respecting the 900 KiB size budget and 256-object count limit
     - Generate deterministic COSL names from COD name + content hash
     - Rewrite COD phase objects: clear inline Object, set ObjectRef to sliceName + identity
     - Return modified COD and COSL apply configurations
   - Implement internal helpers: `parseObjectIdentity`, `gzipData`, content-addressable naming

2. Create `internal/operator-controller/applier/orb/externalizer_test.go`
   - Test no-op path (small COD returns unchanged)
   - Test externalization path (large COD returns modified COD + COSLs)
   - Test COSL splitting by size and by count
   - Test ObjectRef correctness
   - Test assertion/collisionProtection preservation
   - Test gzip compression of large objects
   - Test deterministic naming
   - Test error cases (invalid JSON)
