Review all changes on the current branch for consistency and quality.

## Steps

1. Identify the base branch and get the diff:
   ```
   git diff main...HEAD --name-only
   ```

2. Read all changed files. For each file, check:
   - Does the change follow existing patterns and conventions in the codebase?
   - Are there any security concerns (OWASP top 10, RBAC escalation, injection)?
   - Is error handling consistent with surrounding code?
   - Are tests included for new functionality?

3. Check consistency with governing specs:
   - Read `specs/mission.md` — does the change align with goals and design principles?
   - Read `specs/tech-stack.md` — are the right tools and patterns being used?
   - Read `specs/conventions.md` — do commit messages and code style match conventions?

4. If a phase spec exists for this branch, check against it:
   - Read `specs/*/validation.md` — are validation criteria met?
   - Read `specs/*/requirements.md` — are all required task groups addressed?

5. Check whether any governing documents need updating:
   - Does `AGENTS.md` need updates for new patterns, APIs, or conventions?
   - Do any specs need amendments based on what was learned during implementation?

6. For issues with multiple valid approaches, use AskUserQuestion to present options and let the author decide.

7. Apply straightforward fixes directly:
   - Typos, formatting inconsistencies
   - Missing error checks that follow established patterns
   - Import ordering issues

8. Verify generated files are up-to-date (CRDs, deepcopy, manifests):
   ```
   make verify
   ```

9. Run the check commands to verify the branch is clean:
   ```
   make lint && make test-unit
   ```

10. Summarize findings: what looks good, what needs attention, and any spec updates recommended.
