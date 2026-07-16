Review the current branch's changes for correctness and consistency.

## Step 1: Identify changes

Use `jj diff -r @` and `jj log` to identify all changes on the current bookmark compared to main. Read each changed file.

## Step 2: Find the work item spec

Look for a `specs/YYYY-MM-DD-*/` directory with `status: in-progress`. If found, read all files in the spec directory (README.md, requirements.md, plan.md, verification.md) for context on what the changes should accomplish.

## Step 3: Check correctness

For each changed file:
- Does the code follow Go conventions and the project's design principles from `specs/mission.md`?
- Are there bugs, logic errors, or edge cases missed?
- Are tests adequate for the changes?
- Is the public API surface intentional and minimal?
- Are legacy dependencies (`operator-framework/api`, `operator-framework/operator-registry`) used only where necessary?
- Is there any code that introduces Kubernetes cluster dependencies (kubeconfig, kube client, etc.)?

## Step 4: Check consistency with governing specs

- Does the implementation match the requirements and acceptance criteria in `requirements.md` (if one exists)?
- Was `plan.md` followed correctly?
- Do all checks in `verification.md` pass?
- Is the code consistent with `specs/tech-stack.md` (correct dependencies, project structure)?
- Do change descriptions follow `specs/conventions.md`?
- Does `CLAUDE.md` need updating to reflect new packages, commands, or conventions?

## Step 5: Check for issues

Look for:
- Dead code or unused imports
- Inconsistent naming or patterns across the changeset
- Missing or incomplete test coverage
- Overly broad public API (things that should be in `internal/`)

## Step 6: Run project checks

Run `make lint && make test-unit` to confirm all checks pass. If API types changed, also run `make verify-crd-compatibility`.

## Step 7: Act on findings

- Apply straightforward fixes directly (formatting, obvious bugs, missing error checks).
- Use AskUserQuestion for issues with multiple valid options.
- Summarize any remaining concerns that need the author's judgment.

## Step 8: Update spec status

If the review found no blocking issues and the work item's spec is still `status: in-progress`, use AskUserQuestion to ask whether to set it to `status: done`. If yes, update the README.md frontmatter.

## Step 9: Suggest shipping

If no blocking issues remain, suggest the user run `/sdd-ship` to finalize and publish the changes.
