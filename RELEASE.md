# Release Process for OLM v1

## Choosing version increment
When making releases for operator-controller, version increments should adhere strictly to Semantic Versioning. In short:
* Major: API breaking change(s) are made.
* Minor: Backwards compatible features are added.
* Patch: Backwards compatible bug fix is made.

When a major or minor release being made is associated with one or more milestones, please ensure that all related features have been merged into the `main` branch before continuing.

## Creating the release
Note that throughout this guide, the `upstream` remote refers to the `operator-framework/operator-controller` repository.

The release process differs slightly based on whether a patch or major/minor release is being made.

### Patch Release
#### Step 1
In this example we will be creating a new patch release from version `v1.2.3`, on the branch `release-v1.2`.  
First ensure that the release branch has been updated on remote with the changes from the patch, then perform the following:
```bash
git fetch upstream release-v1.2
git pull release-v1.2
git checkout release-v1.2
```

#### Step 2
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
Once the tag has been pushed the release action should run automatically. You can view the progress [here](https://github.com/operator-framework/operator-lifecycle-manager/actions/workflows/goreleaser.yaml). When finished, the release should then be available on the [releases page](https://github.com/operator-framework/operator-controller/releases).
