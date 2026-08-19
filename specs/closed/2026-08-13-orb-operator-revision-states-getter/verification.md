# Verification

## Implementation Correctness

- [ ] `OrbOperatorRevisionStatesGetter` has a `Reader client.Reader` field, wired
      from `c.mgr.GetClient()` in `main.go`
- [ ] `GetRevisionStates` lists COS via `client.MatchingFields{"spec.group": ext.Name}`
      and `Get`s the COD named `ext.Name` (NotFound tolerated)
- [ ] Revisions are sorted ascending by `Spec.Revision`; archived revisions skipped
- [ ] `RevisionMetadata` is built from COS annotations (name/version/package/image);
      `Release` set only when the key is present
- [ ] `completedAt != nil` classifies a revision as `Installed` (last wins); others
      go to `RollingOut`; a revision with `completedAt == nil` is never `Installed`
- [ ] List/Get errors are returned wrapped; empty list yields empty non-nil
      `RevisionStates`
- [ ] `RevisionMetadata.Conditions` carries a synthesized
      `ClusterObjectSetTypeAvailable` (passthrough) and `ClusterObjectSetTypeProgressing`,
      attached to the CE-Progressing-driving revision
- [ ] Progressing classification matches the README priority table exactly:
      completed -> Succeeded; COD `ProgressDeadlineExceeded` ->
      False/ProgressDeadlineExceeded (wins); phase `Invalid` OR (`synced < total` +
      `objectDetails`) -> Retrying; COD error reasons -> Retrying;
      `WaitingForAssertions` / clean progress -> RollingOut
- [ ] `ApplyBundleWithBoxcutter` renamed to `ApplyBundleWithRevisions`; Boxcutter
      and orb configurators in `cmd/operator-controller/main.go` both call it; Helm
      still calls `ApplyBundle`
- [ ] The orb applier is unchanged except for a clarifying comment; its bool is not
      consulted for status; a non-nil apply error still yields `Retrying`
- [ ] All unit tests pass

## Behavioral / e2e (live experimental cluster)

- [ ] `recover.feature:55` passes under `make test-experimental-e2e` **unchanged**
      - the collision maps to `Progressing=True/Retrying` via the phase-`Invalid`
      cue (confirmed on the live cluster: `deploy` phase `status: Invalid`,
      `synced 0/total 1`, immutable-selector message in `objectDetails`)
- [ ] Fresh install with a stuck revision (COS `completedAt == nil`): CE shows
      `Installed != True` and `Progressing != Succeeded` (premature-success gone)
- [ ] Healthy install: CE goes `Progressing=RollingOut -> Succeeded`, with
      `status.install.bundle` and `status.activeRevisions` populated
- [ ] After install completes, the CE status stops churning (no `BundleDeprecated`
      flap)
- [ ] `install.feature` and `update.feature` happy-path scenarios still pass
- [ ] Boxcutter runtime status behavior unchanged (rely on existing e2e/unit
      coverage)

## Project Conventions

- [ ] Code follows Go style and passes `make lint`
- [ ] No `//nolint` comments added
- [ ] Uses the `labels.*` key constants (not string literals) for annotation lookups
- [ ] Mirrors `BoxcutterRevisionStatesGetter` structure for consistency (per
      specs/mission.md: simple, predictable)
- [ ] Uses orb-operator API types from tech-stack
      (`github.com/joelanford/orb-operator@v0.0.3`); no new dependencies
- [ ] `make test-unit` passes; `make verify` shows no unintended generated-code
      changes
