# Coast-to-coast Demo for Q4 2023

This coast-to-coast demo highlights some of the new features introduced to OLMv1 in Q4 of 2023.

New Features:
- `Catalog` polling
- Version range support on the `spec.version` field for `Operator` resources
- Semver upgrade constraint policy
- `BundleDeployment` health status

This document will go through an example that highlights each of these new features. Each step will be documented as if you are following along and will be done sequentially. This document will also use some values specific to this example and in your own projects should be replaced to use a different value.

## Prerequisites
- A running Kubernetes cluster where you have admin privileges
- `operator-sdk` installed
    - Installation instructions can be found at https://sdk.operatorframework.io/docs/installation/
- `kubectl` installed
    - Installation instructions can be found at https://kubernetes.io/docs/tasks/tools/
- A container runtime of your choice, in this demo we will be using `docker`
- Install `operator-controller` vX.Y.Z 
    - Installation instructions can be found at https://github.com/operator-framework/operator-controller/releases/tag/vX.Y.Z
- `kustomize` installed
    - Installation instructions can be found at https://kubectl.docs.kubernetes.io/installation/kustomize/
- `yq` installed
    - Installation instructions can be found at https://github.com/mikefarah/yq/#install
## Prepare for takeoff

Before we can start exploring the new features we need to do some preparation by creating a new
Operator and building a few different versions.

### Create an Operator
>[!NOTE]
>In this demo, we aren't going to make the Operator do anything.

We will create a new Operator using the `operator-sdk`.

- Create a new directory for the project and `cd` into it:
```sh
mkdir coastal && cd coastal
```

- Initialize the project:
```sh
operator-sdk init --domain coastal.operatorframework.io
```

- Create a new API:
```sh
operator-sdk create api \
    --group coastal.operatorframework.io \
    --version v1alpha1 \
    --kind Coast \
    --resource --controller
```

### Build the Operator Controller and Bundle Images
For this demo we are going to build a several different versions of this Operator:
- `v1.0.0-alpha1`
- `v1.0.0`
- `v1.0.1`
- `v1.1.0`
- `v2.0.0`

Initial setup:
- Create the `manifests/` directory for building the plain bundle
```sh
  mkdir -p manifests
```

- Create a Dockerfile for the plain bundle image
```sh
cat << EOF > plainbundle.Dockerfile
FROM scratch
ADD manifests /manifests
EOF
```

For each of the versions above, perform the following actions:
- Generate the manifests
```sh
  make generate manifests
```

- Build the controller image
```sh
  make docker-build IMG="quay.io/operator-framework/coastal:{VERSION}"
```

- Populate the `manifests/` directory for building the plain bundle
```sh
  kustomize build config/default > manifests/manifests.yaml
```

- Build the plain bundle image
```sh
docker build -t quay.io/operator-framework/coastal-bundle:{VERSION} -f plainbundle.Dockerfile .
```

- Push the controller image and plain bundle image
```sh
docker push quay.io/operator-framework/coastal:{VERSION} && \
docker push quay.io/operator-framework/coastal-bundle:{VERSION}
```

A handy little script to run all these steps for you:
```sh
#!/usr/bin/env bash
set -e

mkdir -p manifests

cat << EOF > plainbundle.Dockerfile
FROM scratch
ADD manifests /manifests
EOF

versions=( v1.0.0-alpha1 v1.0.0 v1.0.1 v1.1.0 v2.0.0 )
for version in "${versions[@]}"
do
  make generate manifests
  make docker-build IMG="quay.io/operator-framework/coastal:${version}"
  mkdir -p manifests
  kustomize build config/default > manifests/manifests.yaml
  docker build -t "quay.io/operator-framework/coastal-bundle:${version}" -f plainbundle.Dockerfile .
  docker push "quay.io/operator-framework/coastal:${version}"
  docker push "quay.io/operator-framework/coastal-bundle:${version}"
done
```

### Create an initial File-Based Catalog (FBC) Image
Now we need to build and push an initial File-Based Catalog (FBC) image containing
our bundles. To help highlight the new polling functionality, we will generate a catalog containing
only the `v1.0.0-alpha1` bundle and we will mimic active development by incrementally updating the catalog
image to include more of our bundles.

- Create a `catalog/` directory
```sh
mkdir catalog
```

- Create a Dockerfile for the catalog image
```sh
cat << EOF > catalog.Dockerfile
FROM scratch
ADD catalog /configs
LABEL operators.operatorframework.io.index.configs.v1=/configs
EOF
```

- Create the FBC YAML file
```sh
cat << EOF > catalog/index.yaml
schema: olm.package
name: coastal
---
schema: olm.bundle
name: coastal.v1.0.0-alpha1
package: coastal
image: quay.io/operator-framework/coastal-bundle:v1.0.0-alpha1
properties:
  - type: olm.package
    value:
      packageName: coastal
      version: 1.0.0-alpha1
  - type: olm.bundle.mediatype
    value: plain+v0
---
schema: olm.channel
name: stable
package: coastal
entries:
  - name: coastal.v1.0.0-alpha1
EOF
```

- Build and push the catalog image
```sh
docker build -t quay.io/operator-framework/coastal-catalog:latest -f catalog.Dockerfile . && \
docker push quay.io/operator-framework/coastal-catalog:latest
```

### Create a `Catalog` Resource
Create a `Catalog` resource that references the catalog image we built
in the previous step and specify a polling interval of 15 seconds. This will make sure that the updates
we make to the catalog image are reflected on our cluster relatively quickly.

- Create the `Catalog` resource:
```sh
kubectl apply -f - <<EOF
apiVersion: catalogd.operatorframework.io/v1alpha1
kind: Catalog
metadata:
  name: coastal
spec:
  source:
    type: image
    image:
      ref: quay.io/operator-framework/coastal-catalog:latest
      pollInterval: 15s
EOF
```

## Only install/upgrade to `v1.0.z` releases
In this section we are going to create an `Operator` resource that attempts to install our Operator
with a version range of `1.0.x`. This version range ensures we only install z-stream releases for `v1.0`
excluding pre-release versions.

- Create the `Operator` resource:
```sh
kubectl apply -f - <<EOF
apiVersion: operators.operatorframework.io/v1alpha1
kind: Operator
metadata:
  name: coastal
spec:
  packageName: coastal
  version: 1.0.x
EOF
```

We should see the `Operator` resource eventually has a failed resolution status. Verify this with:
```sh
kubectl get operator/coastal -o yaml -w
```
>[!NOTE]
>The above command establishes a watch on the `Operator` resource and blocks. Once you are done verifying
>the resolution status you can exit the command with `ctrl+c`

### Update the FBC Image to contain a bundle for `v1.0.0`
To highlight both the polling functionality and the version range constraints, let's add the `v1.0.0` bundle
of our Operator to the catalog image we created in the preparation steps and push the changes.

- Add the new bundle to the catalog YAML file
```sh
cat << EOF >> catalog/index.yaml
---
schema: olm.bundle
name: coastal.v1.0.0
package: coastal
image: quay.io/operator-framework/coastal-bundle:v1.0.0
properties:
  - type: olm.package
    value:
      packageName: coastal
      version: 1.0.0
  - type: olm.bundle.mediatype
    value: plain+v0
EOF
```

- Using `yq`, update the channel to include this bundle as an entry
```sh
yq eval 'select(.schema=="olm.channel" and .name == "stable").entries += [{"name" : "coastal.v1.0.0"}]' -i catalog/index.yaml
```

- Build and push the catalog image
```sh
docker build -t quay.io/operator-framework/coastal-catalog:latest -f catalog.Dockerfile . && \
docker push quay.io/operator-framework/coastal-catalog:latest
```

Shortly, we should see that the `Catalog` resource updates to have a new resolved reference and the `Operator` resource we created previously is successfully installed. 

Verify this for the `Operator` with:
```sh
kubectl get operator/coastal -o yaml -w
```

and for the `Catalog` with:
```sh
kubectl get catalog/coastal -o yaml -w
```

Once the `Operator` has been successfully installed we can verify that all the resources created are healthy by checking the `BundleDeployment` resource owned by the `Operator` we created. Verify the `BundleDeployment` has a status condition showing whether or not it is healthy with:
```sh
kubectl get bundledeployment/coastal -o yaml -w
```

### Update the FBC Image to contain a bundle for `v1.1.0`
Let's ensure that adding a bundle with a version of `v1.1.0` does not trigger an upgrade.

- Add the new bundle to the catalog YAML file
```sh
cat << EOF >> catalog/index.yaml
---
schema: olm.bundle
name: coastal.v1.1.0
package: coastal
image: quay.io/operator-framework/coastal-bundle:v1.1.0
properties:
  - type: olm.package
    value:
      packageName: coastal
      version: 1.1.0
  - type: olm.bundle.mediatype
    value: plain+v0
EOF
```

- Using `yq`, update the channel to include this bundle as an entry
```sh
yq eval 'select(.schema=="olm.channel" and .name == "stable").entries += [{"name" : "coastal.v1.1.0"}]' -i catalog/index.yaml
```

- Build and push the catalog image
```sh
docker build -t quay.io/operator-framework/coastal-catalog:latest -f catalog.Dockerfile . && \
docker push quay.io/operator-framework/coastal-catalog:latest
```

Similar to the previous procedure, the `Catalog` updates its resolved reference,
but the `Operator` resource remains the same and does not 
automatically upgrade to `v1.1.0`

### Update the FBC Image to contain a bundle for `v1.0.1`
Lets add the `v1.0.1` bundle to our catalog 
and ensure it automatically upgrades within the z-stream.

- Add the new bundle to the catalog YAML file
```sh
cat << EOF >> catalog/index.yaml
---
schema: olm.bundle
name: coastal.v1.0.1
package: coastal
image: quay.io/operator-framework/coastal-bundle:v1.0.1
properties:
  - type: olm.package
    value:
      packageName: coastal
      version: 1.0.1
  - type: olm.bundle.mediatype
    value: plain+v0
EOF
```

- Using `yq`, update the channel to include this bundle as an entry
```sh
yq eval 'select(.schema=="olm.channel" and .name == "stable").entries += [{"name" : "coastal.v1.0.1"}]' -i catalog/index.yaml
```

- Build and push the catalog image
```sh
docker build -t quay.io/operator-framework/coastal-catalog:latest -f catalog.Dockerfile . && \
docker push quay.io/operator-framework/coastal-catalog:latest
```

Once again, we should see the `Catalog` update its resolved reference. This time, we expect that the `Operator` resource is automatically upgraded to the new `v1.0.1` bundle we added.

## Change version range to pin installs/upgrades to `v1.1.z` releases
Making changes to the `Operator` resource should result in a re-reconciliation of the resource
which should result in another resolution loop. To see this, let's update the version range
on our `Operator` resource to `1.1.x` with:
```sh
kubectl apply -f - <<EOF
apiVersion: operators.operatorframework.io/v1alpha1
kind: Operator
metadata:
  name: coastal
spec:
  packageName: coastal
  version: 1.1.x
EOF
```

After this change has been applied, the `Operator` resource is updated
and says that we have installed the `v1.1.0` bundle.

You can watch this change happen with:
```sh
kubectl get operator/coastal -o yaml -w
```

## Attempting a major version upgrade by changing the version range
The Operator Controller follows semver and prevents automatic upgrades to new major versions when an `Operator`'s `spec.upgradeConstraintPolicy` is set to `Enforce`. New major versions might
have breaking changes and could cause problems for users. Let's add a new major 
version of our bundle to the catalog image and update the version range on the `Operator`.

### Update the FBC Image to contain a bundle for `v2.0.0`
- Add the new bundle to the catalog YAML file
```sh
cat << EOF >> catalog/index.yaml
---
schema: olm.bundle
name: coastal.v2.0.0
package: coastal
image: quay.io/operator-framework/coastal-bundle:v2.0.0
properties:
  - type: olm.package
    value:
      packageName: coastal
      version: 2.0.0
  - type: olm.bundle.mediatype
    value: plain+v0
EOF
```

- Using `yq`, update the channel to include this bundle as an entry
```sh
yq eval 'select(.schema=="olm.channel" and .name == "stable").entries += [{"name" : "coastal.v2.0.0"}]' -i catalog/index.yaml
```

- Build and push the catalog image
```sh
docker build -t quay.io/operator-framework/coastal-catalog:latest -f catalog.Dockerfile . && \
docker push quay.io/operator-framework/coastal-catalog:latest
```

The `Catalog` updates its resolved reference and no changes are applied to the `Operator` resource.

Updating the `Operator` resource's version range will still result in a resolution failure since the version installed
is a lower major version than specified by the version range. Try it by running:
```sh
kubectl apply -f - <<EOF
apiVersion: operators.operatorframework.io/v1alpha1
kind: Operator
metadata:
  name: coastal
spec:
  packageName: coastal
  version: 2.0.x
EOF
```

Watch for a resolution failure with:
```sh
kubectl get operator/coastal -o yaml -w
```

## To the escape hatch!
To tell operator-controller to ignore the semver policies and allow upgrades across major versions,
set the `Operator`'s `spec.upgradeConstraintPolicy` to `Ignore` with:
```sh
kubectl apply -f - <<EOF
apiVersion: operators.operatorframework.io/v1alpha1
kind: Operator
metadata:
  name: coastal
spec:
  packageName: coastal
  upgradeConstraintPolicy: Ignore
  version: 2.0.x
EOF
```

We should see that eventually the `Operator` will resolve and install the `v2.0.0` bundle we added to the
catalog image in the previous step. Watch this happen with:
```sh
kubectl get operator/coastal -o yaml -w
```

## Attempting to downgrade by changing the version range
We can disable downgrades by setting the `Operator` resource's `spec.UpgradeConstraintPolicy` field to `Enforce`.
To see this, run:
```sh
kubectl apply -f - <<EOF
apiVersion: operators.operatorframework.io/v1alpha1
kind: Operator
metadata:
  name: coastal
spec:
  packageName: coastal
  upgradeConstraintPolicy: Enforce
  version: 1.1.x
EOF
```

We should see resolution fail since it is attempting to downgrade. Watch this happen with:
```sh
kubectl get operator/coastal -o yaml -w
```

## Back to the escape hatch!
To tell operator-controller to ignore the safety mechanisms and downgrade the `Operator` version,
set the `Operator`'s `spec.upgradeConstraintPolicy` to `Ignore` with:
```sh
kubectl apply -f - <<EOF
apiVersion: operators.operatorframework.io/v1alpha1
kind: Operator
metadata:
  name: coastal
spec:
  packageName: coastal
  upgradeConstraintPolicy: Ignore
  version: 1.1.x
EOF
```

We should see that eventually the `Operator` will resolve and install the `v1.1.0` bundle we added to the
catalog image in a previous step.  Watch this happen with:
```sh
kubectl get operator/coastal -o yaml -w
```