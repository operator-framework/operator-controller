# Releasing RukPkak

This guide outlines the release process for the rukpak project.

## Overview

The rukpak project uses [GoReleaser](https://goreleaser.com/) to automatically produce multi-arch container images and release artifacts during the release process.

In order to create a new minor version release, simply create a valid [semver tag][semver] locally, push that tag up to the upstream repository remote, and let automation handle the rest.

The release automation will be responsible for producing manifest list container images, generating rendered Kubernetes manifests, and a GithHub release that will contain both of those attached as release artifacts.

## Publishing a new release

### Steps

- Review the current release milestone
- Create and push a new release tag
- Verify the goreleaser action was successful

#### Review the current release milestones

Ensure that the current release [milestone][milestone] is accurately reflected, and all tickets are in the closed state. When a release milestone has open tickets, reach out to the [rukpak-dev][slack] channel to see which tickets can be removed from the release.

#### Create and push a new release tag

In order to trigger the existing release automation, you need to first create a new tag locally, and push that tag up to the upstream repository remote.

- Make sure you are on `main` branch and the staging directory is clean with no local changes
- Pull the latest commits from the upstream `main` branch
- Make a new tag that matches the version
- Push tag directly to this repository

**Note**: The following steps assume that the upstream rukpak repository remote is named `upstream`. Replace the v0.3.0 example tag with the correct tag when running these commands locally.

```bash
git checkout main
git pull --rebase upstream main
# Create an annotated tag as `git describe` only uses annotated tags by default.
git tag -a v0.3.0 -m "v0.3.0 release"
git push upstream v0.3.0
```

#### Verify that GoReleaser is Running

Once a manual tag has been created, monitor the progress of the [release workflow action][workflow] that was triggered when a new tag has been created to ensure a successful run.

Once that release workflow has run, navigate to the [quay.io/operator-framework/rukpak image repository][image] and ensure that the expected container images have been created.

[milestone]: <https://github.com/operator-framework/rukpak/milestones>
[release]: <https://github.com/operator-framework/rukpak/releases>
[slack]: <https://kubernetes.slack.com/archives/C038B7MF75M>
[workflow]: <https://github.com/operator-framework/rukpak/actions/workflows/release.yaml>
[image]: <https://quay.io/repository/operator-framework/rukpak?tab=tags>
[semver]: <https://semver.org/>
