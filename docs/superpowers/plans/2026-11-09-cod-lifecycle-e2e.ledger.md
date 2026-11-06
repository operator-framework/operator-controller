# Ledger: COD Lifecycle E2E (OPRUN-4794)

**Plan:** docs/superpowers/plans/2026-11-09-cod-lifecycle-e2e.md
**Branch:** cod-lifecycle-e2e  **BASE:** 77754d81

## Rulings

- Ruling: Work on a feature branch in the existing operator-controller checkout instead of a separate git worktree — because the plan doc and superpowers workspace already live in this checkout's untracked tree and a worktree would separate them. Cost if wrong: branch shares the working dir with main; low risk since changes are additive test-only feature files.
- Ruling: Per-task green verification (`make e2e/<feature>`) requires `make experimental-e2e-setup` (a KIND cluster), which the human acknowledged is out of band. Substitute per-task verification: static check that every Gherkin step in the new/edited feature files resolves to a step regex registered in test/e2e/steps/steps.go (catches the "undefined step" failure mode without a cluster). Cost if wrong: a step that resolves statically could still fail at runtime behaviorally; mitigated by matching wording to steps already used by passing cod_*.feature scenarios.

## Rulings (cont.)

- Ruling: The plan/AC's "edit root deadline only -> no new revision" has no valid target: a root-level `spec.progressDeadlineMinutes` is pruned by the CRD (proved by TestClusterObjectDeploymentDeferredFieldsArePruned); the only deadline lives at `spec.template.spec.progressDeadlineMinutes`, which IS in the template hash and would cut a new revision. Substitute real non-hashed root fields: edit `metadata.labels` and `spec.revisionHistoryLimit`. Cost if wrong: if revisionHistoryLimit were ever folded into the template hash this scenario would start cutting a revision and fail loudly (acceptable 
 it would surface a real semantics change).

## Risks / runtime caveats

- Handoff scenario asserts the blocked successor is Ready=False/ObjectCollision (the real state in this controller), not Ready=Unknown as the AC loosely worded it. The intent (successor blocked, predecessor retained) is preserved.
- Deletion-teardown scenario (Task 3) encodes AC4 behavior, but the epic lists "teardown/orphan-deletion edge cases" as still-open. It may FAIL at runtime; if so, that is a real gap to report, not a test defect. Human to decide whether to keep as expected-pass or tag pending.
- All per-task verification was static (step-resolution via /tmp/check_steps.py) per the no-cluster ruling. Behavioral green requires `make experimental-e2e-setup` then `make e2e/cod_rollout|cod_handoff|cod_lifecycle`.

## Task log

- Task 1 (cod_rollout.feature): DONE 
 3 scenarios added + initial-rollout owns-1 assertion. Static steps resolve. Commit made.
- Task 2 (cod_handoff.feature): DONE 
 2 scenarios. Static steps resolve. Commit made.
- Task 3 (cod_lifecycle.feature): DONE 
 2 scenarios. Static steps resolve. Commit made.
