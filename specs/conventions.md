# Conventions

## Commit Messages

Prefix commit subjects with an emoji shortcode indicating the type of change:

| Prefix | Meaning |
|---|---|
| `:warning:` | Major/breaking change |
| `:sparkles:` | Minor/compatible change (new feature) |
| `:bug:` | Bug fix |
| `:book:` | Documentation |
| `:seedling:` | Other (dependency bumps, chores, refactoring) |

Format:
```
:prefix: Short summary of the change (#issue)

Optional longer description explaining motivation and context.
```

Examples:
```
:sparkles: Add version pinning support for ClusterExtension (#1234)

:bug: Fix missing olm.operatorNamespace annotation (#2803)

:seedling: Bump github.com/klauspost/compress from 1.18.6 to 1.19.0 (#2815)

:warning: Remove HelmChartSupport feature (#2798)
```

Guidelines:
- Keep the subject line concise (under ~72 characters after the prefix)
- Reference the GitHub issue number when applicable
- Use imperative mood ("Add support" not "Added support")
- Body is optional but encouraged for non-trivial changes

## Pull Requests

### Title Format

PR titles use the same emoji prefix as commit messages:

```
:sparkles: Add version pinning support for ClusterExtension
:bug: Fix missing annotation on namespace-scoped resources
```

### Description

Follow the PR template (`.github/pull_request_template.md`):

1. **Description** - Summary of changes and motivation
2. **Reviewer Checklist**:
   - API Go documentation
   - Tests (unit tests, and e2e tests if appropriate)
   - Comprehensive commit messages
   - Links to related GitHub issues

### Reviewer Expectations

- API changes need Go doc comments on exported types and fields
- Non-trivial changes should include unit tests at minimum
- Changes affecting user-facing behavior should include e2e tests
- Each commit in the PR should be self-contained and pass CI independently

## Branch Naming

No strict convention enforced. Common patterns used in the project:

```
joe/short-description
fix/issue-number-description
feature/short-name
```
