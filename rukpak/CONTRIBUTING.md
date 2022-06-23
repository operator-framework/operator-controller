# How to contribute

Rukpak is an Apache 2.0 licensed project and accepts contributions via GitHub pull requests (PRs).

This document outlines some conventions on commit message formatting, contact points for developers, and other resources
to help RukPak contributors.

## Communication

- Email: [operator-framework-olm-dev](mailto:operator-framework-olm-dev@googlegroups.com)
- Slack: [#rukpak-dev](https://kubernetes.slack.com/archives/C038B7MF75M)
- Working Group: [olm-wg](https://groups.google.com/g/operator-framework-olm-dev)

## Getting started

- Fork the repository on GitHub.
- Clone onto your local development machine via `git clone https://github.com/{$GH-USERNAME}/rukpak`.
- Add the operator-framework/rukpak as an upstream.
  remote: `git remote add upstream https://github.com/operator-framework/rukpak`.
- Create a new branch from the default `main` branch and begin development.

## Reporting bugs and creating issues

Any new contribution should be accompanied by a new or existing issue. This issue can help track work, discuss the
design and implementation, and help avoid wasted efforts of multiple people working on the same issue. Trivial changes,
like fixing a typo in the documentation, do not require the creation of a new issue.

Proposing larger changes to the RukPak project may require an enhancement proposal, or some documentation, before being
considered. Any change to RukPak's existing behavior or features, APIs, or changes and additions to tests do not require
an enhancement proposal.

## Contribution flow

This is a rough outline of what a contributor's workflow looks like:

- Identify or create an issue.
- Create a topic branch from where to base the contribution. This is usually the main branch.
- Make commits of logical units.
- Make sure commit messages are in the proper format (see below).
- Push changes in a topic branch to a personal fork of the repository.
- Submit a pull request to the operator-framework/rukpak repository.
- Wait and respond to feedback from the maintainers listed in the OWNERS file.

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

Currently, PRs required at least one approval from a RukPak maintainer in order to get merged.

### Code style

The coding style suggested by the Golang community is used throughout the RukPak project:

- [CodeReviewComments](https://github.com/golang/go/wiki/CodeReviewComments)
- [EffectiveGo](https://golang.org/doc/effective_go)

In addition to the linked style documentation, RukPak formats Golang packages using the `golangci-lint` tool. Before
submitting a PR, please run `make lint` locally and commit the results. This will help expedite the review process,
focusing less on style conflicts, and more on the design and implementation details.

Please follow this style to make the RukPak project easier to review, maintain and develop.

### Documentation

If the contribution changes the existing APIs or user interface it must include sufficient documentation to explain the
new or updated features.

The RukPak documentation is primarily housed at the root-level README, and there is also provisioner-specific
documentation located in the [provisioner repository](./internal/provisioner/README.md).
