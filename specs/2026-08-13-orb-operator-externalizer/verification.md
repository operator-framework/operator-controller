# Verification

## Implementation Correctness

- [ ] `Externalize` returns the COD unchanged with nil slices when under the size threshold
- [ ] `Externalize` rewrites the COD and returns COSLs when over the size threshold
- [ ] Extracts apiVersion, kind, name, namespace from each raw JSON object
- [ ] Builds SliceObject entries with correct identity and content
- [ ] Respects the 900 KiB per-COSL size budget
- [ ] Respects the 256 per-COSL object count limit
- [ ] All SliceObject content is gzip-compressed unconditionally
- [ ] COSL names are deterministic and content-addressable
- [ ] ObjectRef entries correctly reference sliceName and object identity
- [ ] CollisionProtection and Assertions are preserved on PhaseObjects
- [ ] Nil/empty raw extensions are skipped without error
- [ ] All unit tests pass

## Project Conventions

- [ ] Code follows Go style and passes `make lint`
- [ ] No `//nolint` comments added
- [ ] Package structure matches project layout (`internal/operator-controller/applier/orb/`)
- [ ] Uses existing orb-operator apply configuration types (`orbac`) consistently
- [ ] Size constants are documented with rationale
- [ ] No unnecessary abstractions or interfaces
- [ ] Follows design principles from specs/mission.md (simple, predictable, works with Kubernetes)
- [ ] Uses tech stack from specs/tech-stack.md (controller-runtime types, orb-operator dependency)
