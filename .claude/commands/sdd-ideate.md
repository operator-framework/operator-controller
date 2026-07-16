Brainstorm and add new work items to the backlog.

## Step 1: Gather context

1. Read `specs/mission.md` to understand project goals and non-goals.
2. Read `specs/tech-stack.md` to understand the current tech stack.
3. List all `specs/YYYY-MM-DD-*/` directories and read their README.md files to understand existing work items and their statuses.
4. Briefly summarize the current state: what exists, what's in progress, what's done, and where the backlog currently stands.

## Step 2: Surface unrefined items

If there are existing work items with `status: idea`, list them and use AskUserQuestion to ask whether the user wants to refine any of them further before brainstorming new ideas. If so, use AskUserQuestion iteratively to flesh out the idea with more detail, then update its README.md accordingly (keeping `status: idea` - refinement to `ready` happens in `/sdd-plan-next-phase`).

## Step 3: Brainstorm

If the user provided input via $ARGUMENTS, use that as a starting theme.

Use AskUserQuestion to explore:
- Problems or gaps in the current codebase
- User requests or feature ideas
- Technical debt worth addressing
- Mission goals that aren't yet addressed by any work item
- Dependencies between potential items

## Step 4: Propose candidates

For each candidate work item, present:
- **Short name** (slug-friendly)
- **One-line description**
- **3-6 deliverable bullets**
- **Why it matters** (connection to mission goals)

## Step 5: Iterate

Use AskUserQuestion to refine:
- Split large items into smaller ones
- Merge overlapping items
- Reorder by priority
- Check for dependencies between items
- Verify none conflict with non-goals from `specs/mission.md`

## Step 6: Create work items

For each finalized item, create `specs/YYYY-MM-DD-<slug>/README.md`:

```markdown
---
status: idea
---
# <Title>

<One or two sentence description of the idea.>
```

Use today's date for the `YYYY-MM-DD` prefix.

## Step 7: Summary

Summarize what was added to the backlog. Suggest running `/sdd-plan-next-phase` to refine and start the next item.
