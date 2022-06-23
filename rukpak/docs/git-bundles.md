# Git based bundles

## Summary

A Bundle can reference content in a remote git repository instead of a remote container image by using the
`git` source type in the Bundle manifest. This enables one to easily source content from a git provider like GitHub and
make it available within the cluster.

For example, for a plain+v0 bundle, the git repository backing the bundle, or "bundle git repo", must have a certain
structure in order to produce a valid Bundle that works with the plain provisioner. It should have a manifests directory
where the Kubernetes manifests are stored -- this directory is used when copying manifests onto the cluster. By default,
the `manifests` directory is assumed to be at the root level, but a different bundle root can be specified
via `spec.source.git.directory`.

> Note: There must be a `manifests` directory rooted in the provided directory in order to have a valid bundle git repo.

For an example of a plain+v0 bundle git repo that conforms to the specifications, see
the [combo repository](https://github.com/operator-framework/combo/).

When creating a Bundle from a git source, a reference to a particular commit, tag, or branch must be provided for the
provisioner to know where the bundle content is stored in the repository. Only one can be specified, and it is expected
that the manifests are present in the particular commit/tag/branch at the directory specified.

Currently only publicly-available git repositories are available as backing sources to Bundles. Authentication and using
private git repositories as Bundle sources is on the roadmap.

## Examples

plain+v0 Bundle the references a git repository by a commit:

```yaml
apiVersion: core.rukpak.io/v1alpha1
kind: Bundle
metadata:
  name: combo-commit-ref
spec:
  source:
    type: git
    git:
      ref:
        commit: d40082c96e6f0d297aa316d84020d307f95dc453
      repository: https://github.com/operator-framework/combo
  provisionerClassName: core.rukpak.io/plain
```

plain+v0 Bundle that references a git repository by a tag:

```yaml
apiVersion: core.rukpak.io/v1alpha1
kind: Bundle
metadata:
  name: combo-tag-ref
spec:
  source:
    type: git
    git:
      ref:
        tag: v0.0.2
      repository: https://github.com/operator-framework/combo
  provisionerClassName: core.rukpak.io/plain
```

plain+v0 Bundle that references a git repository by a branch:

```yaml
apiVersion: core.rukpak.io/v1alpha1
kind: Bundle
metadata:
  name: combo-branch-ref
spec:
  source:
    type: git
    git:
      ref:
        branch: main
      repository: https://github.com/operator-framework/combo
  provisionerClassName: core.rukpak.io/plain
```

plain+v0 Bundle that has a different manifest directory than the default:

```yaml
apiVersion: core.rukpak.io/v1alpha1
kind: Bundle
metadata:
  name: combo-branch-ref-deploy
spec:
  source:
    type: git
    git:
      ref:
        branch: main
      directory: ./dev/deploy
      repository: https://github.com/exdx/combo-bundle
  provisionerClassName: core.rukpak.io/plain
```

