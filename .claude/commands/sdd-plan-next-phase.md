Plan the next development phase for operator-controller.

## Steps

1. Check for uncommitted changes with `git status`. If there are uncommitted changes, use AskUserQuestion to ask whether to stash them (`git stash`) or abort.

2. Check out `main` and pull latest from upstream:
   ```
   git checkout main && git pull upstream main
   ```

3. Read `specs/roadmap.md` and find the next undefined phase (look for "TBD" or the last numbered phase and increment).

4. Create a new branch:
   ```
   git checkout -b phase-{N}-{short-name}
   ```

5. Use AskUserQuestion to gather details about the upcoming phase:
   - What is the focus area for this phase?
   - What are the key deliverables (3-6 items)?
   - Are there any dependencies or blockers?
   - What validation criteria define "done" for this phase?

6. Create the phase spec directory and files:
   ```
   specs/YYYY-MM-DD-phase-{N}-{name}/
     plan.md          — overview, goals, approach
     requirements.md  — detailed requirements and task groups
     validation.md    — acceptance criteria and test plan
   ```

7. Reference `specs/mission.md` for alignment with project goals and design principles. Reference `specs/tech-stack.md` for implementation constraints (build commands, test frameworks, CI requirements).

8. After writing the spec files, run a self-review:
   - Check internal consistency across plan, requirements, and validation
   - Verify alignment with mission goals and non-goals
   - Ensure deliverables are concrete and testable
   - Flag any gaps or ambiguities using AskUserQuestion
