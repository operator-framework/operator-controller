Implement a work item from its spec.

## Step 1: Identify the work item

If the user provided input via $ARGUMENTS, use that to find the spec directory.

Otherwise, list `specs/YYYY-MM-DD-*/` directories with `status: ready` or `status: in-progress` in their README.md frontmatter. Use AskUserQuestion to ask which one to implement.

## Step 2: Read the spec

Read all files in the work item's spec directory:
- `README.md` - summary and design
- `requirements.md` - functional requirements and acceptance criteria
- `plan.md` - implementation plan
- `verification.md` - verification criteria

If the spec is incomplete (still an `idea` or missing files), stop and tell the user to run `/sdd-plan-next-phase` to refine it first.

## Step 3: Update status

Set the frontmatter `status: in-progress` in the work item's README.md.

## Step 4: Implement

Follow the implementation plan in `plan.md` task groups in order. For each task group:

1. Start a new jj change for this task group: `jj new -m ":sparkles: <description>"` (use the appropriate emoji prefix from `specs/conventions.md`).
2. Read `specs/mission.md` for design principles and `specs/tech-stack.md` for technical guidance.
3. Implement the changes for this task group. Changes are automatically tracked by jj.
4. Run `make lint && make test-unit` to verify nothing is broken.
5. Use AskUserQuestion for any decisions not covered by the spec.
6. When done, the current jj change already contains the committed work. Start the next task group with another `jj new -m "..."`.

## Step 5: Verify

After implementation is complete:

1. Walk through each check in `verification.md` and confirm it passes.
2. Walk through each acceptance criterion in `requirements.md` and confirm it is met.
3. Run `make lint && make test-unit` one final time.
4. Run `make verify` to ensure generated code is up-to-date.
5. If any verification check or criterion fails, fix it in the change where the issue was introduced (navigate with `jj edit` and changes are applied automatically).

## Step 6: Update governing docs

Review whether anything learned during implementation should be reflected in global SDD documents (`specs/mission.md`, `specs/tech-stack.md`, `specs/conventions.md`, `CLAUDE.md`, or any other top-level specs). If updates are needed, make them in a dedicated change separate from the implementation changes.

## Step 7: Suggest review

Suggest the user run `/sdd-review` to review the changes for correctness and consistency before shipping.
