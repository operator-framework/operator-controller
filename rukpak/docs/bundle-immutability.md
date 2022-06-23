# Bundle Immutability

## Overview

A Bundle object, once accepted by the api-server, is considered an immutable artifact by the rest of the rukpak system.
This behavior is meant to enforce the notion that a Bundle represents some unique, static piece of content that should
be sourced onto the cluster. A user can have confidence, therefore, that a particular Bundle is pointing to a specific
set of manifests, and cannot be updated without creating a new Bundle. This property is true for both standalone Bundles
and dynamic Bundles created via an embedded BundleTemplate.

Bundle immutability is enforced via the core rukpak webhook. This webhook watches Bundle events, and for any update to a
Bundle, checks whether the spec of the existing Bundle is semantically equal to that in the proposed updated Bundle. If
they are not equal, the update is rejected by the webhook. Note that other Bundle fields, such as metadata or status,
can and will be updated during the Bundle's lifecycle -- it's only the spec that is considered immutable.

## Example

Applying a Bundle and then attempting to update its spec should fail. Let's see this in action by first creating a
Bundle:

```console
kubectl apply -f -<<EOF
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
EOF
```

The Bundle should have been created successfully.

```console
bundle.core.rukpak.io/combo-tag-ref created
```

Next, let's try to patch the Bundle to point to a newer tag:

```console
kubectl patch bundle combo-tag-ref --type='merge' -p '{"spec":{"source":{"git":{"ref":{"tag":"v0.0.3"}}}}}'
```

The following error is returned:

```console
Error from server (bundle.spec is immutable): admission webhook "vbundles.core.rukpak.io" denied the request: bundle.spec is immutable
```

It's clear from the error that the core rukpak admission webhook rejected the patch, as the spec of the bundle is
immutable. The recommended way to change the content of a Bundle is by creating a new Bundle, versus updating it
in-place.

## Further considerations

While the spec of the Bundle is immutable, it's still possible to run into scenarios where a BundleInstance pivots to a
newer version of bundle content without changing the underlying Bundle spec.

This unintentional pivoting could occur when:

1. Using an image tag, a git branch, or a git tag in the Bundle source
2. Moving the image tag to a new digest, pushing changes to a git branch, or deleting and re-pushing a git tag on a
   different commit
3. Doing something to cause the Bundle's unpack pod to be re-created (for example, deleting the unpack pod)

If a user performs these steps, the new content from step 2 will be unpacked as a result of step 3. The BundleInstance
would notice the changes, and pivot to the newer version of the content.

This is similar to pod behavior, where one of the pod's container images uses a tag, the tag is moved to a different
digest, and then at some point in the future the existing pod gets rescheduled on a different node. At that point the
node pulls the new image at the new digest and runs something different without the user explicitly asking for it.

To be absolutely confident that the underlying Bundle content does not change, use a digest-based image or a git commit
reference when creating the Bundle.
