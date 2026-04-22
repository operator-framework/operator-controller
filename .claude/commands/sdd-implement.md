Implement the current phase spec for operator-controller.

## Steps

1. Identify the active phase spec. Look for a `specs/*/plan.md` file on the current branch. If multiple exist or none are found, use AskUserQuestion to clarify which phase to implement.

2. Read all spec files for the phase:
   - `plan.md` — understand the goals and approach
   - `requirements.md` — follow task groups in order
   - `validation.md` — know the acceptance criteria upfront

3. Implement task groups in the order specified in `requirements.md`. For each task group:
   - Read the requirements carefully before writing code
   - Follow existing patterns in the codebase (see `AGENTS.md` for conventions)
   - Use AskUserQuestion for any decisions not covered by the spec

4. After implementing each task group, verify the relevant validation criteria from `validation.md`.

5. Run the project check commands after implementation:
   ```
   make lint && make test-unit
   ```

6. If code generation is needed (API changes, CRD changes):
   ```
   make generate && make manifests
   ```

7. Run format to ensure consistency:
   ```
   make fmt
   ```

8. Verify all generated code is up-to-date:
   ```
   make verify
   ```

9. If validation criteria require e2e tests and a kind cluster is available:
   ```
   make test-e2e
   ```

10. Use AskUserQuestion for any implementation decisions not covered by the spec, referencing `specs/mission.md` design principles and `specs/tech-stack.md` constraints.
