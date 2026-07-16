Clean up completed specs: update statuses, backfill PR links, archive done work, and flag stale ideas.

## Step 1: Inventory specs

1. List all directories under `specs/` matching `YYYY-MM-DD-*/`.
2. Read the `README.md` frontmatter in each to get its status and pr field.
3. Categorize into: `done` (with pr), `done` (without pr), `pr-submitted`, `in-progress`, `ready`, `idea`.
4. Present a summary table to the user.

## Step 2: Promote merged pr-submitted specs

For each spec with `status: pr-submitted`:

1. If it has a `pr:` field, check whether the linked PR has been merged (use `gh pr view`).
2. If it has no `pr:` field, search for a merged PR referencing the spec slug (same approach as Step 3).
3. If the PR is merged, update the README.md frontmatter to `status: done` (keep or add the `pr:` field).
4. Report which specs were promoted.

## Step 3: Backfill PR links on done specs

For each spec with `status: done` and no `pr:` field:

1. Search for a merged PR whose title or body references the spec slug (use `gh pr list --state merged --search "<slug>"`).
2. If exactly one PR matches, add `pr: <URL>` to the frontmatter.
3. If multiple match, present the candidates and use AskUserQuestion to let the user pick.
4. If none match, note it in the summary (some specs predate the PR workflow — that's fine, skip them).

## Step 4: Archive done specs

1. Create `specs/closed/` if it doesn't exist.
2. Move every spec directory with `status: done` into `specs/closed/`.
3. Report how many specs were archived.

## Step 5: Flag stale ideas

For each spec with `status: idea`:

1. Check when it was last modified (use `jj log -r 'ancestors(@)' --no-graph -T 'commit_id ++ "\n"' -n 1 -- <spec-dir>` or fall back to `git log -1 --format=%ci -- <spec-dir>`).
2. If last touched more than 30 days ago, flag it as stale.
3. Present any stale ideas to the user and use AskUserQuestion to ask whether to drop, keep, or refine each one.

## Step 6: Commit

1. Use AskUserQuestion to confirm before committing.
2. Commit all changes with message: `chore: clean up completed specs`.

## Step 7: Summary

Report what was done:
- Specs promoted from pr-submitted to done
- PR links backfilled
- Specs archived to specs/closed/
- Stale ideas flagged (and any actions taken)
- Remaining active specs (in-progress, ready, idea)
