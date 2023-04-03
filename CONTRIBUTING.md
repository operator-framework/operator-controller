# How to contribute

Operator Controller is an Apache 2.0 licensed project and accepts contributions via GitHub pull requests (PRs).

This document outlines some conventions on commit message formatting, contact points for developers, and other resources
to help Operator Controller contributors.

## Communication

- Email: [operator-framework-olm-dev](mailto:operator-framework-olm-dev@googlegroups.com)
- Slack: [#olm-dev](https://kubernetes.slack.com/archives/C0181L6JYQ2)
- Google Group: [olm-gg](https://groups.google.com/g/operator-framework-olm-dev)
- Weekly in Person Working Group Meeting: [olm-wg](https://github.com/operator-framework/community#operator-lifecycle-manager-working-group) 

## Contribution flow

Any new contribution should be accompanied by a new or existing issue. This issue can help track work, discuss the
design and implementation, and help avoid wasted efforts of multiple people working on the same issue. Trivial changes,
like fixing a typo in the documentation, do not require the creation of a new issue.

### Small Contributions

For simple contributions, this is a rough outline of what a contributor's workflow looks like:

- Identify or create a GitHub Issue.
- Create a topic branch from where to base the contribution. This is usually the main branch.
- Make commits of logical units, be sure to include tests, and sign each commit to satisfy [the DCO](https://github.com/cncf/foundation/blob/main/dco-guidelines.md).
- Make sure commit messages are in the proper format (see [code review](#code-review)).
- Push changes in a topic branch to a personal fork of the repository.
- Submit a PR to the operator-framework/operator-controller repository.
- Wait and respond to feedback from the maintainers listed in the OWNERS file, we will do our best to respond promptly
but encourage you to tag us on the PR if it isn't addressed within a week.

### Large Contributions

The Operator Controller project is an open source project featuring contributors from many companies and backgrounds.
In an effort to coordinate ongoing efforts from multiple contributors, large scale features are tracked on the
[OLM V1 GitHub Project](https://github.com/orgs/operator-framework/projects/8/views/14?pane=info) in a `Milestone #` tab.
Unassigned tickets in a `Milestone #` are available for contributors to take, simply assign the ticket to yourself and
reach out to the community by commenting on the issue or by messaging us on the [#olm-dev](https://kubernetes.slack.com/archives/C0181L6JYQ2) Slack Channel.

If you are interested in proposing a "focus" for a milestone, the contribution workflow should be similar to the following:

- Create a [GitHub Issue](https://github.com/operator-framework/operator-controller/issues/new), labeled with "milestone-proposal", that includes a link to a [HackMD](https://hackmd.io) capturing the proposed changes.
- Add an item to the [OLM Working Group and Issue Triage Meeting Agenda](https://docs.google.com/document/d/1Zuv-BoNFSwj10_zXPfaS9LWUQUCak2c8l48d0-AhpBw/edit) to introduce your design to the community.
- Introduce your proposed changes at the weekly [OLM Working Group Meeting](https://github.com/operator-framework/community#operator-lifecycle-manager-working-group) 

The community will then discuss the proposal, consider the benefits and cost of the feature, iterate on the design, and decide if it project should pursue the design. If accepted, the design will be queued up for a subsequent milestone.

Thanks for contributing!

### Code Review

Contributing PRs with a reasonable title and description can go a long way with helping the PR through the review
process.

When opening PRs that are in a rough draft or WIP state, prefix the PR description with `WIP: ...` or create a draft PR.
This can help save reviewer's time by communicating the state of a PR ahead of time. Draft/WIP PRs can be a good way to
get early feedback from reviewers on the implementation, focusing less on smaller details, and more on the general
approach of changes.

When contributing changes that require a new dependency, check whether it's feasible to directly vendor that
code [without introducing a new dependency](https://go-proverbs.github.io/).

Currently, PRs require at least one approval from a operator-controller maintainer in order to get merged.

### Code style

The coding style suggested by the Golang community is used throughout the operator-controller project:

- [CodeReviewComments](https://github.com/golang/go/wiki/CodeReviewComments)
- [EffectiveGo](https://golang.org/doc/effective_go)

In addition to the linked style documentation, operator-controller formats Golang packages using the `golangci-lint` tool. Before
submitting a PR, please run `make lint` locally and commit the results. This will help expedite the review process,
focusing less on style conflicts, and more on the design and implementation details.

Please follow this style to make the operator-controller project easier to review, maintain and develop.

### Documentation

If the contribution changes the existing APIs or user interface it must include sufficient documentation to explain the
new or updated features.

The Operator Controller documentation is primarily housed at the root-level README.
