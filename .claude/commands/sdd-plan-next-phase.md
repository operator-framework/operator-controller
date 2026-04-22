Plan the next development phase for operator-controller by selecting an eligible GitHub epic.

## Steps

1. Check for uncommitted changes with `git status`. If there are uncommitted changes, use AskUserQuestion to ask whether to stash them (`git stash`) or abort.

2. Check out `main` and pull latest from upstream:
   ```
   git checkout main && git pull upstream main
   ```

3. Search for eligible epics on the upstream repository. An eligible epic has labels `epic` and `refined`, is unassigned, and has no unresolved dependencies:
   ```
   gh issue list --repo operator-framework/operator-controller --label epic --label refined --assignee "" --state open --json number,title,body,labels,url --limit 20
   ```

4. For each candidate epic, check its linked issues to verify dependencies are resolved. Use the GitHub API to inspect linked issues:
   ```
   gh api repos/operator-framework/operator-controller/issues/{number}/timeline --jq '.[] | select(.event=="cross-referenced" or .event=="connected")'
   ```
   An epic is eligible only if all blocking/dependency issues have the `done` label. Epics with no dependencies are also eligible.

5. Present the eligible epics to the user via AskUserQuestion, showing the issue number, title, and a brief summary. Let the user pick which epic to work on.

6. Assign the selected epic to the current git user:
   ```
   gh issue edit {number} --repo operator-framework/operator-controller --add-assignee @me
   ```

7. Create a new branch named after the epic:
   ```
   git checkout -b epic-{number}-{short-name}
   ```

8. Use AskUserQuestion to gather implementation details about the epic:
   - What is the approach for this epic?
   - How should the work be broken into task groups?
   - What validation criteria define "done"?

9. Create the phase spec directory and files:
   ```
   specs/{epic-number}-{name}/
     plan.md          — overview, goals, approach (link back to the GitHub issue)
     requirements.md  — detailed requirements and task groups
     validation.md    — acceptance criteria and test plan
   ```

10. Reference `specs/mission.md` for alignment with project goals and design principles. Reference `specs/tech-stack.md` for implementation constraints (build commands, test frameworks, CI requirements).

11. After writing the spec files, run a self-review:
    - Check internal consistency across plan, requirements, and validation
    - Verify alignment with mission goals and non-goals
    - Ensure deliverables are concrete and testable
    - Flag any gaps or ambiguities using AskUserQuestion
