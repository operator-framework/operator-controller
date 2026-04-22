Verify, commit, and publish the current branch for operator-controller.

## Phase 1: Verify

1. Run the project check commands:
   ```
   make lint && make test-unit
   ```

2. Run format to ensure consistency:
   ```
   make fmt
   ```

3. Verify all generated code is up-to-date:
   ```
   make verify
   ```

4. If a phase spec exists for this branch, read `specs/*/validation.md` and verify all acceptance criteria are met.

5. Check if `AGENTS.md` needs updating for any new patterns, APIs, or conventions introduced in this branch.

6. If any verification step fails, fix the issue before proceeding. Use AskUserQuestion if the fix is ambiguous.

## Phase 2: Commit

1. Read `specs/conventions.md` for commit message and PR format requirements.

2. Review all staged and unstaged changes:
   ```
   git status
   git diff
   ```

3. Use AskUserQuestion to confirm the commit plan:
   - Show a summary of what will be committed
   - Propose a commit message following conventions (no emoji prefix on commits; that's for PR titles)
   - Ask whether to create a single commit or multiple logical commits

4. If there are post-review fixup commits that should be squashed, handle squashing into logical commits. Preserve DCO sign-off trailers (`Signed-off-by`) when squashing. Use AskUserQuestion to confirm before any interactive rebase.

5. Ensure all commits have DCO sign-off (`-s` flag).

## Phase 3: Publish

1. Check the current branch. If on `main`, use AskUserQuestion to warn that shipping directly to main is not allowed — a feature branch is required. Abort if confirmed.

2. Use AskUserQuestion to confirm before pushing. Show:
   - The branch name
   - Number of commits ahead of main
   - Summary of changes

3. Push the branch to the fork:
   ```
   git push -u origin <branch-name>
   ```

4. Determine the PR title emoji prefix based on the change type:
   - `:warning:` for breaking changes
   - `:sparkles:` for new features
   - `:bug:` for bug fixes
   - `:book:` for documentation
   - `:seedling:` for everything else

5. Create the PR against upstream using the project's template format:
   ```
   gh pr create --title "<emoji> <title>" --body "$(cat <<'EOF'
   # Description

   <Summary of changes and motivation>

   ## Reviewer Checklist

   - [ ] API Go Documentation
   - [ ] Tests: Unit Tests (and E2E Tests, if appropriate)
   - [ ] Comprehensive Commit Messages
   - [ ] Links to related GitHub Issue(s)
   EOF
   )"
   ```

6. If this branch is linked to a GitHub epic (check `specs/*/plan.md` for an issue reference), use AskUserQuestion to ask whether to add the `done` label to the epic:
   ```
   gh issue edit {number} --repo operator-framework/operator-controller --add-label done
   ```

7. Report the PR URL when done.
