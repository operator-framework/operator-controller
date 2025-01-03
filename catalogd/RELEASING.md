# Release Guide

These steps describe how to cut a release of the catalogd repo.

## Table of Contents:

- [Major and minor releases](#major-and-minor-releases)

## Major and Minor Releases

Before starting, ensure the milestone is cleaned up. All issues that need to
get into the release should be closed and any issue that won't make the release
should be pushed to the next milestone.

These instructions use `v0.Y.0` as the example release. Please ensure to replace
the version with the correct release being cut. It is also assumed that the upstream
operator-framework/catalogd repository is the `upstream` remote on your machine.

### Procedure

1. Create a release branch by running the following, assuming the upstream
operator-framework/catalogd repository is the `upstream` remote on your machine:

   - ```sh
     git checkout main
     git fetch upstream
     git pull upstream main
     git checkout -b release-v0.Y
     git push upstream release-v0.Y
     ```

2. Tag the release:

   - ```sh
     git tag -am "catalogd v0.Y.0" v0.Y.0
     git push upstream v0.Y.0
     ```

3. Check the status of the [release GitHub Action](https://github.com/operator-framework/catalogd/actions/workflows/release.yaml).
Once it is complete, the new release should appear on the [release page](https://github.com/operator-framework/catalogd/releases).

4. Clean up the GitHub milestone:
   - In the [GitHub milestone](https://github.com/operator-framework/catalogd/milestones), bump any open issues to the following release and then close out the milestone.
