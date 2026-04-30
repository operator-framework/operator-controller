# Conventions

## Commit Messages

Format:

```
<High-level description>

<Optional detailed description explaining the why, not the what>

Signed-off-by: Your Name <your@email.com>
```

Example:

```
Refactor Boxcutter controller to rely on kubernetes for resource cleanup

Instead of manually tracking and deleting owned resources, rely on
Kubernetes garbage collection via owner references. This simplifies
the controller logic and eliminates a class of race conditions during
concurrent reconciles.

Signed-off-by: Jane Doe <jane@example.com>
```

Rules:
- DCO sign-off (`Signed-off-by`) is required on all commits
- Keep the subject line concise and descriptive
- Use the body to explain motivation when non-obvious
- Generated files must be committed alongside the source changes that require them

## Pull Requests

### Title Format

PR titles must use an emoji prefix indicating the change type:

| Emoji | Shortcode | Meaning |
|---|---|---|
| ⚠ | `:warning:` | Major/breaking change |
| ✨ | `:sparkles:` | Minor/compatible change (new feature) |
| 🐛 | `:bug:` | Patch/bug fix |
| 📖 | `:book:` | Documentation |
| 🌱 | `:seedling:` | Other (deps, chores, refactors) |

Examples:
- `:sparkles: Add support for deploying OCI helm charts in OLM v1`
- `:bug: Fix race condition in Helm to Boxcutter migration during OLM upgrades`
- `:warning: Remove support for annotation based config`
- `:seedling: Upgrade boxcutter from v0.11.0 to v0.12.0`

### Description Template

```markdown
# Description

<Summary of changes and motivation>

## Reviewer Checklist

- [ ] API Go Documentation
- [ ] Tests: Unit Tests (and E2E Tests, if appropriate)
- [ ] Comprehensive Commit Messages
- [ ] Links to related GitHub Issue(s)
```

### Requirements

- Must pass all CI checks (unit-test, e2e, sanity, lint)
- Must have both `approved` and `lgtm` labels (from repository approvers and reviewers)
- Draft PRs: prefix with "WIP:" or use GitHub draft feature

## Branch Naming

- **Main branch:** `main` (default, protected)
- **Release branches:** `release-v{MAJOR}.{MINOR}` (e.g., `release-v1.2`)
- **Feature branches:** Created from `main`, typically in contributor forks, merged via PR

## Issue Labels

| Label | Purpose |
|---|---|
| `epic` | Marks an issue as an epic (a significant body of work) |
| `refined` | Indicates the epic has been refined and is ready for implementation |
| `done` | Marks a completed epic; used to resolve dependency chains |

An epic is eligible for work when it has both `epic` and `refined` labels, is unassigned, and all linked blocking issues carry the `done` label.

## Git Remotes

Fork-based development workflow:
- **`origin`** — your personal fork (push here)
- **`upstream`** — the project repository `operator-framework/operator-controller` (pull from here)

To sync with upstream: `git pull upstream main`
To push a branch: `git push -u origin <branch-name>`
