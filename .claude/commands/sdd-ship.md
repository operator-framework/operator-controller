Finalize and publish the current branch's changes.

## Phase 1: Verify

1. Run `make lint` - all linting must pass.
2. Run `make test-unit` - all unit tests must pass.
3. Run `make verify` - all generated code must be up-to-date.
4. Run `make fmt` - code must be formatted.
5. If any API types were changed, run `make verify-crd-compatibility` to check backward compatibility.
6. If a work item spec exists (`specs/YYYY-MM-DD-*/` with `status: in-progress`), verify all acceptance criteria are met.
7. Check that `CLAUDE.md` is up to date with any new packages or conventions.

If any check fails, stop and report the issue.

## Phase 2: Commit

1. Read `specs/conventions.md` for commit and PR format.
2. Review the jj log on this bookmark. If there are fixup changes that should be squashed, squash them (use `jj squash -m "combined description"` or `jj squash -u` to avoid editor prompts).
3. Ensure each change has a well-formed description following `specs/conventions.md` (emoji prefix, imperative mood, issue reference).
4. Use AskUserQuestion to confirm the commit history looks correct before proceeding.

## Phase 3: Publish

1. Use AskUserQuestion to confirm before pushing.
2. Push the bookmark with `jj git push` and create a PR following the conventions in `specs/conventions.md`:
   - Title: emoji-prefixed summary matching the primary change
   - Body: Description of changes and motivation, plus the Reviewer Checklist from the PR template
   - Link to the work item spec directory if one exists
3. If a work item spec exists, update its frontmatter:
   - Set `status: pr-submitted`
   - Add `pr: <PR URL>`

## Phase 4: Monitor CI

1. Spawn a background agent to watch the PR's CI checks. The agent should:
   - Poll the PR's check runs periodically until they complete.
   - Report back with any failures, including the failing check name and a summary of the error.
2. Summarize: PR URL, what was shipped, and that CI is being monitored in the background.
