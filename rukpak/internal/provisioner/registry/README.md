# Registry Provisioner

## Summary

The `registry` provisioner is a core Rukpak provisioner that knows how to interact with bundles of a particular format.
These `registry+v1` bundles, or `registry` bundles, are container images containing a set of static Kubernetes YAML
manifests organized in the legacy Operator Lifecycle Manger (OLM) format. For more information on the `registry+v1` format, see
the [OLM packaging doc](https://olm.operatorframework.io/docs/tasks/creating-operator-manifests/).

The `registry` provisioner is able to convert a given `registry+v1` bundle onto a cluster in the `plain+v0` format. Instantiation of the
bundle is then handled by the `plain` provisioner in order to make the content available in the cluster. It does so by reconciling `Bundle`
types that have the `spec.provisionerClassName` field set to `core.rukpak.io/registry`. This field must be set to the correct provisioner
name in order for the `registry` provisioner to see and interact with the bundle. Creating a `BundleInstance` of that `Bundle` would then
require that you have set the `BundleInstance` property `spec.provisionerClassName` field to `core.rukpak.io/plain`. For a concrete example
of this in action, see the [use cases section](#use-cases)

> Note: Not all `registry+v1` content is supported. This mainly applies to `registry+v1` bundles that enable `AllNamespaces` mode
or include a webhook.

## Use cases

### Install and apply a specific version of a `registry+v1` bundle

> :warning: Anyone with the ability to create or update BundleInstance objects can become cluster admin. It's important
> to limit access to this API via RBAC to only those that explicitly require access, as well as audit your bundles to
> ensure the content being installed on-cluster is as-expected and secure.

The `registry` provisioner can convert and make available a specific `registry+v1` bundle as a `plain+v0` in the cluster.

Simply create a `BundleInstance` resource that contains the desired specification of a Bundle resource.
The `registry` provisioner will unpack the provided Bundle onto the cluster, and the `plain` provisioner
will eventually make the content available on the cluster.

```yaml
apiVersion: core.rukpak.io/v1alpha1
kind: BundleInstance
metadata:
  name: my-bundle-instance
spec:
  provisionerClassName: core.rukpak.io/plain
  template:
    metadata:
      labels:
        app: my-bundle
    spec:
      source:
        type: image
        image:
          ref: my-bundle@sha256:xyz123
      provisionerClassName: core.rukpak.io/registry
```

> Note: The generated Bundle will contain the BundleInstance's metadata.Name as a prefix, followed by
> the hash of the provided template.

First, the Bundle will be in the Pending stage as the provisioner sees it and begins unpacking the referenced content:

```console
$ kubectl get bundle my-bundle
NAME           TYPE    PHASE      AGE
my-bundle      image   Pending    3s
```

Then eventually, as the bundle content is unpacked onto the cluster via the defined storage mechanism, the bundle status
will be updated to Unpacked, indicating that all its contents have been stored on-cluster.

```console
$ kubectl get bundle my-bundle
NAME           TYPE    PHASE      AGE
my-bundle      image   Unpacked   10s
```

Now that the bundle has been unpacked, the provisioner is able to create the resources in the bundle on the cluster.
These resources will be owned by the corresponding BundleInstance. Creating the BundleInstance on-cluster results in an
InstallationSucceeded Phase if the application of resources to the cluster was successful.

```console
$ kubectl get bundleinstance my-bundle-instance
NAME                 DESIRED BUNDLE   INSTALLED BUNDLE   INSTALL STATE           AGE
my-bundle-instance   my-bundle        my-bundle          InstallationSucceeded   11s
```

> Note: Creation of more than one BundleInstance from the same Bundle will likely result in an error.

## Running locally

### Setup

To experiment with the `registry` provisioner locally, take the following steps to
create a local [kind](https://kind.sigs.k8s.io/) cluster and deploy the provisioner onto it:

```bash
# Clone the repository
git clone https://github.com/operator-framework/rukpak

# Navigate to the repository
cd rukpak

# Start a local kind cluster then build and deploy the provisioner onto it
make run
```

### Installing the Prometheus Operator

From there, create some Bundles and BundleInstance types to see the provisioner in action. For an example bundle to
use, the `prometheus` operator is a good example.

Create the `prometheus` BundleInstance referencing the desired `prometheus` Bundle configuration:

```bash
kubectl apply -f -<<EOF
apiVersion: core.rukpak.io/v1alpha1
kind: BundleInstance
metadata:
  name: prometheus
spec:
  provisionerClassName: core.rukpak.io/plain
  template:
    metadata:
      labels:
        app: prometheus
    spec:
      provisionerClassName: core.rukpak.io/registry
      source:
        type: image
        image:
          ref: quay.io/operatorhubio/prometheus:v0.47.0--20220325T220130
EOF
```

A message saying that the BundleInstance is created should be returned:

```console
$ kubectl apply -f -<<EOF
...
EOF
bundleinstance.core.rukpak.io/prometheus created
```

Next, check the Bundle status via:

```bash
kubectl get bundle -l app=prometheus
```

Eventually the Bundle should show up as Unpacked:

```console
$ kubectl get bundle -l app=prometheus
NAME                    TYPE    PHASE      AGE
prometheus-5699cbff6   image   Unpacked   14s
```

Check the BundleInstance status to ensure that the installation was successful:

```bash
kubectl get bundleinstance prometheus
```

A successful installation will show InstallationSucceeded as the `INSTALL STATE`:

```console
$ kubectl get bundleinstance prometheus
NAME         INSTALLED BUNDLE        INSTALL STATE           AGE
prometheus   prometheus-5699cbff6    InstallationSucceeded   10s
```

From there, check out the prometheus operator deployment and ensure that the operator is present on the cluster:

```bash
# Check the prometheus operator deployment
kubectl -n prometheus-system get deployments.apps prometheus-operator

# Check that the operator is present
kubectl -n prometheus-system get deployments.apps prometheus-operator -o yaml | grep 'image:' | xargs
```

The deployment should show that the operator is ready and available:

```console
$ kubectl -n prometheus-system get deployments.apps prometheus-operator
NAME                  READY   UP-TO-DATE   AVAILABLE   AGE
prometheus-operator   1/1     1            1           86m
```

This means the operator should be successfully installed.

The `plain` provisioner continually reconciles BundleInstance resources and, since the `registry+v1` bundle got converted into a `plain+v0` format, this also applies. Next, let's try deleting the `prometheus` deployment:

```bash
kubectl -n prometheus-system delete deployments.apps prometheus-operator
```

A message saying the deployment was deleted should be returned:

```console
$ kubectl -n prometheus-system delete deployments.apps prometheus-operator
deployment.apps "prometheus-operator" deleted
```

The provisioner ensures that all resources required for the BundleInstance to run are accounted for on-cluster.
So if we check for the deployment again, it will be back on the cluster:

```console
$ kubectl -n prometheus-system get deployments.apps prometheus-operator
NAME                  READY   UP-TO-DATE   AVAILABLE   AGE
prometheus-operator   1/1     1            1           15s
```

### Upgrading the Prometheus Operator

Let's say the `prometheus` operator released a newer release and we want to upgrade to that version.

> Note: Upgrading a BundleInstance involves updating the desired Bundle template being referenced.

Update the existing `prometheus` BundleInstance resource and update the container image being referenced:

```bash
kubectl apply -f -<<EOF
apiVersion: core.rukpak.io/v1alpha1
kind: BundleInstance
metadata:
  name: prometheus
spec:
  provisionerClassName: core.rukpak.io/plain
  template:
    metadata:
      labels:
        app: prometheus
    spec:
      provisionerClassName: core.rukpak.io/registry
      source:
        type: image
        image:
          ref: quay.io/operatorhubio/prometheus:v0.47.0--20220413T184225
EOF
```

Once the newly generated Bundle is reporting an Unpacked status, the BundleInstance `prometheus` resource should now
point to the new Bundle (now named `prometheus-7f4f468d94` instead of `prometheus-5699cbff6` previously). The prometheus-operator
deployment in the prometheus-system namespace should also be healthy and contain a new container image:

```console
$ kubectl get bundles -l app=prometheus
NAME                    TYPE    PHASE      AGE
prometheus-7f4f468d94   image   Unpacked   2m15s

$ kubectl get bundleinstance prometheus
NAME         INSTALLED BUNDLE        INSTALL STATE           AGE
prometheus   prometheus-7f4f468d94   InstallationSucceeded   2m47s

$ kubectl -n prometheus-system get deployment
NAME                  READY   UP-TO-DATE   AVAILABLE   AGE
prometheus-operator   1/1     1            1           3m6s
```

### Deleting the Prometheus Operator and Local Kind Cluster

To clean up from the installation, simply remove the BundleInstance from the cluster. This will remove all references
resources including the deployment, RBAC, and the operator namespace.

> Note: There's no need to manually clean up the Bundles that were generated from a BundleInstance resource. The plain provisioner places owner references on any Bundle that's generated from an individual BundleInstance resource.

```bash
# Delete the prometheus BundleInstance
kubectl delete bundleinstances.core.rukpak.io prometheus
```

A message should show that the BundleInstance was deleted and now the cluster state is the same as it was
prior to installing the operator.

```console
$ kubectl delete bundleinstances.core.rukpak.io prometheus
bundleinstance.core.rukpak.io "prometheus" deleted
```

To stop and clean up the kind cluster, delete it:

```bash
# Clean up kind cluster
make kind-cluster-cleanup
```
