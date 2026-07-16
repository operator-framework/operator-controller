Quickly capture a work item idea to the backlog.

## Step 1: Get the idea

If the user provided input via $ARGUMENTS, use that as the idea description.

Otherwise, use AskUserQuestion to ask the user to describe the idea in one or two sentences.

## Step 2: Generate a slug

Derive a short, descriptive slug from the idea (lowercase, hyphens, no special characters).

## Step 3: Create the work item

Create `specs/YYYY-MM-DD-<slug>/README.md` using today's date:

```markdown
---
status: idea
---
# <Title>

<The user's idea description.>
```

## Step 4: Confirm

Report the created file path. Suggest `/sdd-plan-next-phase` to refine it when ready.
