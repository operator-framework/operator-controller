# Requirements

- `ExternalizeCOD` returns the COD unchanged (with nil slices) when the serialized size is under the safe etcd threshold (900 KiB)
- `ExternalizeCOD` converts all inline objects into COSLs and rewrites the COD when over the threshold
- Each COSL stays under the 900 KiB data budget to leave headroom for apiserver-added metadata
- Each COSL holds at most 256 SliceObject entries (API maximum)
- COSL names are deterministic and content-addressable: `<cod-name>-<sha256-prefix>`
- Object identity (apiVersion, kind, name, namespace) is extracted from inline raw JSON for each SliceObject and ObjectRef
- All SliceObject content is gzip-compressed unconditionally
- CollisionProtection and Assertions on PhaseObjects are preserved through externalization
- Objects with empty/nil raw extension data are skipped
- Each produced COSL inherits the COD's metadata labels (or none, if the COD has no labels)
- Each produced COSL inherits the COD's owner references (or none, if the COD has none)

## Acceptance Criteria

- Unit test: COD under size limit returns unchanged COD and nil slices
- Unit test: COD over size limit returns modified COD and non-nil slices
- Unit test: produces correct COSL count when objects fit in one slice
- Unit test: produces multiple COSLs when total content exceeds the per-COSL budget
- Unit test: produces multiple COSLs when object count exceeds 256 per slice
- Unit test: ObjectRef entries correctly identify each object by apiVersion, kind, name, namespace, and sliceName
- Unit test: Assertions and CollisionProtection are preserved on PhaseObjects after replacement
- Unit test: SliceObject content is always gzip-compressed
- Unit test: COSLs inherit the COD's labels; no labels when the COD has none
- Unit test: COSLs inherit the COD's owner references; none when the COD has none
- Unit test: COSL names are deterministic (same input produces same names)
- Unit test: Duplicate content (same raw JSON) within a single COSL is not deduplicated (each object gets its own SliceObject entry since they have distinct identities)
- Unit test: Invalid/unparseable raw JSON returns an error
