# How to Contribute

Operator Controller is an Apache 2.0 licensed project and accepts contributions via GitHub pull requests (PRs).

## Certificate of Origin

By contributing to this project you agree to the Developer Certificate of
Origin (DCO). This document was created by the Linux Kernel community and is a
simple statement that you, as a contributor, have the legal right to make the
contribution. See the [DCO](https://github.com/operator-framework/operator-controller/blob/main/DCO) file for details.

## Overview

Thank you for your interest in contributing to the Operator-Controller.

As you may or may not know, the Operator-Controller project aims to deliver the user experience described in the [Operator Lifecycle Manager (OLM) V1 Product Requirements Document (PRD)](https://docs.google.com/document/d/1-vsZ2dAODNfoHb7Nf0fbYeKDF7DUqEzS9HqgeMCvbDs/edit). The design requirements captured in the OLM V1 PRD were born from customer and community feedback based on the experience they had with the released version of [OLM V0](https://github.com/operator-framework/operator-lifecycle-manager).

The user experience captured in the OLM V1 PRD introduces many requirements that are best satisfied by a microservices architecture. The OLM V1 experience currently relies on two components:

- [Operator-Controller](https://github.com/operator-framework/operator-controller/), which is the top level component allowing users to specify operators they'd like to install.
- [Catalogd](https://github.com/operator-framework/operator-controller/tree/main/catalogd), which hosts operator content and helps users discover installable content.

## How do we collaborate

> "We need to accept that random issues and pull requests will show up" - Joe L.

Before diving into our process for coordinating community efforts, I think it's important to set the expectation that Open Source development can be messy. Any effort to introduce a formal workflow for project contributions will almost certainly be circumvented by new community users. Rather than pestering users to subscribe to a project-specific process, we strive to make it as simple as possible to provide valuable feedback. With that in mind, changes to the project will almost certainly follow this process:

1. The community engages in discussion in the [#olm-dev](https://kubernetes.slack.com/archives/C0181L6JYQ2) slack channel.
2. The community creates GitHub Issues, GitHub Discussions, or pull requests in the appropriate repos based on (1) to continue the discussion.
3. The community utilizes the Working Group Meeting to talk about items from (1) and (2) as well as anything else that comes to mind.

The workflow defined above implies that the community is always ready for discussion and that ongoing work can be found in the GitHub repository as GitHub Issues, GitHub Discussions, or pull requests, and that milestone planning is async, happening as part of (1), (2), and (3).

Please keep this workflow in mind as you read through the document.

## How to Build and Deploy Locally

After creating a fork and cloning the project locally,
you can follow the steps below to test your changes:

1. Create the cluster:

    ```sh
    kind create cluster -n operator-controller
    ```

2. Build your changes:

    ```sh
    make build docker-build
    ```

3. Load the image locally and Deploy to Kind

    ```sh
    make kind-load kind-deploy
    ```

## How to debug controller tests using ENVTEST

[ENVTEST](https://book.kubebuilder.io/reference/envtest) requires k8s binaries to be downloaded to run the tests.
To download the necessary binaries, follow the steps below:

```sh
make envtest-k8s-bins
```

Note that the binaries are downloaded to the `bin/envtest-binaries` directory.

```sh
$ tree
.
├── envtest-binaries
│   └── k8s
│       └── 1.31.0-darwin-arm64
│           ├── etcd
│           ├── kube-apiserver
│           └── kubectl
```

Now, you can debug them with your IDE:

![Screenshot IDE example](https://github.com/user-attachments/assets/3096d524-0686-48ca-911c-5b843093ad1f)

### Communication Channels

- Email: [operator-framework-olm-dev](mailto:operator-framework-olm-dev@googlegroups.com)
- Slack: [#olm-dev](https://kubernetes.slack.com/archives/C0181L6JYQ2)
- Google Group: [olm-gg](https://groups.google.com/g/operator-framework-olm-dev)
- Weekly in Person Working Group Meeting: [olm-wg](https://github.com/operator-framework/community#operator-lifecycle-manager-working-group)

## How are Milestones Designed?

It's unreasonable to attempt to consider all of the design requirements laid out in the [OLM V1 PRD](https://docs.google.com/document/d/1-vsZ2dAODNfoHb7Nf0fbYeKDF7DUqEzS9HqgeMCvbDs/edit) from the onset of the project. Instead, the community attempts to design Milestones with the following principles:

- Milestones are tightly scoped units of work, ideally lasting one to three weeks.
- Milestones are derived from the OLM V1 PRD.
- Milestones are "demo driven", meaning that a set of acceptance criteria is defined upfront and the milestone is done as soon as some member of the community can run the demo.
- Edge cases found during development are captured in issues and assigned to the GA Milestone, which contains a list of issues that block the release of operator-controller v1.0.0 .

This "demo driven" development model will allow us to collect user experience and regularly course correct based on user feedback. Subsequent milestone may revert features or change the user experience based on community feedback.

The project maintainer will create a [GitHub Discussion](https://github.com/operator-framework/operator-controller/discussions) for the upcoming milestone once we've finalized the current milestone. Please feel encouraged to contribute suggestions for the milestone in the discussion.

## Where are Operator Controller Milestones?

Ongoing or previous Operator-Controller milestones can always be found in the [milestone section of our GitHub Repo](https://github.com/operator-framework/operator-controller/milestones).

### How are Subproject Issues Tracked?

As discussed earlier, the operator-controller adheres to a microservice architecture, where multiple projects contribute to the overall experience. As such, when designing an operator-controller milestone, the community may need to file an issue against Catalogd. Unfortunately, the operator-controller milestone cannot contain issues from one of its subprojects. As such, we've introduced the concept of a "Dependency Issue", described below:

> Dependency Issues: An issue tracked in a milestone that "points" to an issue in another project with a URL.

## Submitting Issues

Unsure where to submit an issue?

- [Operator-Controller](https://github.com/operator-framework/operator-controller/), which contains both components, is the project allowing users to specify operators they'd like to install.

## Submitting Pull Requests

### Code Review

Contributing PRs with a reasonable title and description can go a long way with helping the PR through the review
process.

When opening PRs that are in a rough draft or WIP state, prefix the PR description with `WIP: ...` or create a draft PR.
This can help save reviewer's time by communicating the state of a PR ahead of time. Draft/WIP PRs can be a good way to
get early feedback from reviewers on the implementation, focusing less on smaller details, and more on the general
approach of changes.

When contributing changes that require a new dependency, check whether it's feasible to directly vendor that
code [without introducing a new dependency](https://go-proverbs.github.io/).

Currently, PRs require at least one approval from an operator-controller maintainer in order to get merged.

### Code style

The coding style suggested by the Golang community is used throughout the operator-controller project:

- [CodeReviewComments](https://github.com/golang/go/wiki/CodeReviewComments)
- [EffectiveGo](https://golang.org/doc/effective_go)

In addition to the linked style documentation, operator-controller formats Golang packages using the `golangci-lint` tool. Before
submitting a PR, please run `make lint` locally and commit the results. This will help expedite the review process,
focusing less on style conflicts, and more on the design and implementation details.

Please follow this style to make the operator-controller project easier to review, maintain and develop.

### Go version

Our goal is to minimize disruption by requiring the lowest possible Go language version. This means avoiding updaties to the go version specified in the project's `go.mod` file (and other locations).

There is a GitHub PR CI job named `go-verdiff` that will inform a PR author if the Go language version has been updated. It is not a required test, but failures should prompt authors and reviewers to have a discussion with the community about the Go language version change. 

There may be ways to avoid a Go language version change by using not-the-most-recent versions of dependencies. We do acknowledge that CVE fixes might require a specific dependency version that may have updated to a newer version of the Go language.

### Documentation

If the contribution changes the existing APIs or user interface it must include sufficient documentation to explain the
new or updated features.

The Operator Controller documentation is primarily housed at the root-level [README](https://github.com/operator-framework/operator-controller/blob/main/README.md).
