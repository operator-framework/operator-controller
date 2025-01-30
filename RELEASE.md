# Release Process for OLM v1

## Choosing version increment

The `operator-controller` following Semantic Versioning guarantees:

* Major: API breaking change(s) are made.
* Minor: Backwards compatible features are added.
* Patch: Backwards compatible bug fix is made.

When a Major or Minor release being made is associated with one or more milestones,
please ensure that all related features have been merged into the `main` branch before continuing.

## Creating the release

Note that throughout this guide, the `upstream` remote refers to the `operator-framework/operator-controller` repository.
The release process differs slightly based on whether a patch or major/minor release is being made.

### Patch Release

In this example, we will be creating a new patch release from version `v1.2.3` on the branch `release-v1.2`.

#### Step 1 
First, make sure the `release-v1.2` branch is updated with the latest changes from upstream:
```bash
git fetch upstream release-v1.2
git checkout release-v1.2
git reset --hard upstream/release-v1.2
```

#### Step 2
Run the following command to confirm that your local branch has the latest expected commit:
```bash
git log --oneline -n 5
```
Check that the most recent commit matches the latest commit in the upstream `release-v1.2` branch. 

#### Step 3
Create a new tag, incrementing the patch number from the previous version. In this case, we'll be incrementing from `v1.2.3` to `v1.2.4`:
```bash
## Previous version was v1.2.3, so we bump the patch number up by one
git tag v1.2.4
git push upstream v1.2.4
```

### Major/Minor Release
#### Step 1
Create a release branch from `main` branch for the target release. Follow the pattern `release-<MAJOR>.<MINOR>` when naming the branch; for example: `release-v1.2`. The patch version should not be included in the branch name in order to facilitate an easier patch process.
```bash
git checkout main
git fetch upstream main
git pull main
git checkout -b release-v1.2
git push upstream release-v1.2
```

#### Step 2
Create and push our tag for the current release.
```bash
git tag v1.2.0
git push upstream v1.2.0
```

### Post-Steps
Once the tag has been pushed the release action should run automatically. You can view the progress [here](https://github.com/operator-framework/operator-controller/actions/workflows/release.yaml). When finished, the release should then be available on the [releases page](https://github.com/operator-framework/operator-controller/releases).
