# How to Contribute

Catalogd is [Apache 2.0 licensed](LICENSE.md) and accepts contributions via
GitHub pull requests. This document puts together some guidelines on how to make
contributions to Catalogd and provides contacts to get in touch with the
Catalogd maintainers and developers. To learn more about Catalogd and how it
fits into `OLM V1`, please refer to [Operator Lifecycle Manager (OLM) V1 Product
Requirements Document
(PRD)](https://docs.google.com/document/d/1-vsZ2dAODNfoHb7Nf0fbYeKDF7DUqEzS9HqgeMCvbDs/edit#heading=h.dbjdp199nxjk).

## Certificate of Origin

By contributing to this project you agree to the Developer Certificate of Origin
(DCO). This document was created by the Linux Kernel community and is a simple
statement that you, as a contributor, have the legal right to make the
contribution. See the
[DCO](https://github.com/operator-framework/catalogd/blob/main/DCO) file for
details.

## Communication Channels

- Mailing List:
  [operator-framework-olm-dev](mailto:operator-framework-olm-dev@googlegroups.com)
- Slack: [#olm-dev](https://kubernetes.slack.com/archives/C0181L6JYQ2)
- Working Group Meeting:
  [olm-wg](https://groups.google.com/g/operator-framework-olm-dev)

## Getting started

- Fork the repository on GitHub.
- Clone the repository onto your local development machine via `git clone
  https://github.com/{$GH-USERNAME}/catalogd.git`.
- Add `operator-framework/catalogd` upstream remote by running `git remote add
  upstream https://github.com/operator-framework/catalogd.git`.
- Create a new branch from the default `main` branch and begin development in
  the area of your interest.

## How to Build and Deploy Locally

After creating a fork and cloning the project locally,
you can follow the steps below to test your changes:

1. Create the cluster:

    ```sh
    kind create cluster -n catalogd
    ```

2. Build your changes:

    ```sh
    make build-container
    ```

3. Load the image locally and Deploy to Kind

    ```sh
    make kind-load deploy
    ```

## Reporting bugs and creating issues

Any new contribution should be linked to a new or existing github issue in the
Catalogd project. This issue can help track work, discuss the design and
implementation, and help avoid duplicate efforts of multiple people working on
the same issue. Trivial changes, like fixing a typo in the documentation, do not
require the creation of a new issue but can be linked to an existing issue if it
exists.

Proposing larger changes to the Catalogd project may require an enhancement
proposal, or some documentation, before being considered. The maintainers
typically use Google Docs and an RFC process to write design drafts for any new
features. If you're interested in proposing ideas for new features, you can use
the [RFC
Template](https://docs.google.com/document/d/1aYFGdq3W3UKzkRbNopISIdzABh-o5S0et7q0h2qPFGw/edit#heading=h.x3tfh25grvnv)
to write an RFC and place it in the [Design Docs
Folder](https://drive.google.com/drive/u/1/folders/1c5jSCrXuE9bziZcEiIX3X89OEC5tRgEg).

Any changes to Catalogd's existing behavior or features, APIs, or changes and
additions to tests do not require an enhancement proposal.

## Contribution flow

Below is a rough outline of what a contributor's workflow looks like:

- Identify an existing issue or create a new issue. If you're new to Catalogd
  and looking to make a quick contribution and ramp up on the project, a good
  way to do this is to identify `good-first-issues` from [Catalogd Github
  Issues](https://github.com/operator-framework/catalogd/issues) by using the
  filter by label and search for "good first issue". Also, please feel empowered
  to work on other issues that interest you which are not good first issues.
- Create a new branch from the branch you would like to base your contribution
  from. Typically, this is the main branch.
- Create commits that are of logical units.
- Commit your changes and make sure to use commit messages that are in proper
  format following best practices and guidelines for git commit messages (see
  below).
- Once you've committed your changes, push the new branch that contains your
  changes to your personal fork of the repository.
- Submit a pull request to `operator-framework/catalogd` repository.
- Respond to feedback from the Catalogd maintainers and community members and
  address any review suggestions. The PR must receive an approval from at least
  one Catalogd maintainer.

### Format of the commit message

We follow a rough convention for commit messages that is designed to answer two
questions: what changed and why. The subject line should feature the what and
the body of the commit should describe the why.

```text
(feature): create a minimal client library

- Fixes #10
- Add unit tests for the client library
```

The format can be described more formally as follows:

```text
<subsystem>: <what changed>
<BLANK LINE>
<why this change was made>
<BLANK LINE>
<footer>
```

The first line is the subject and should be no longer than 70 characters, the
second line is always blank, and other lines should be wrapped at 80 characters.
This allows the message to be easier to read on GitHub as well as in various git
tools.

### Code Review

Creating PRs with a reasonable title and description can go a long way with
helping the PR through the review process.

When opening PRs that are in a rough draft or work-in-progress (WIP) state,
prefix the PR description with `WIP: ...` or create a draft PR. This can help
save reviewer's time by communicating the state of a PR ahead of time. Draft/WIP
PRs can be a good way to get early feedback from reviewers on the
implementation, focusing less on smaller details, and more on the general
approach of changes.

When contributing changes that require a new dependency, check whether it's
feasible to directly vendor that code [without introducing a new
dependency](https://go-proverbs.github.io/).

Currently, PRs require at least one approval from a Catalogd maintainer in order
to get merged.

### Code style

The coding style suggested by the Golang community is used throughout the
Catalogd project:

- [CodeReviewComments](https://github.com/golang/go/wiki/CodeReviewComments)
- [EffectiveGo](https://golang.org/doc/effective_go)

In addition to the linked style documentation, Catalogd formats Golang packages
using the `golangci-lint` tool. Before submitting a PR, please run `make lint`
locally and commit the results. This will help expedite the review process,
focusing less on style conflicts, and more on the design and implementation
details.

Please follow this style to make the Catalogd project easier to review, maintain
and develop.

### Documentation

If a contribution changes the existing APIs or user interface it should include
sufficient documentation to explain the new or updated features.

The Catalogd documentation is primarily housed at the root-level README, and
there is also demo located in the [catalog docs
directory](https://github.com/operator-framework/catalogd/blob/main/docs/demo.gif).
Any changes to the existing APIs or the user interface would require updating
this demo as well.

### Additional resources

If you would like to view the roadmap of OLMV1, please refer to [Operator
Lifecycle Manager (OLM) V1
Roadmap](https://github.com/orgs/operator-framework/projects/8/views/26) for any
other potential areas of interest in this space.

Thank you for your interest in contributing to the Catalogd project.
