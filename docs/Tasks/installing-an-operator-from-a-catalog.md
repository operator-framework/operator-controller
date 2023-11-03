# Installing an Operator from a catalog in OLM 1.0

Cluster administrators can add _catalogs_, or curated collections of Operators and Kubernetes extensions, to their clusters. Operator authors publish their products to these catalogs. When you add a catalog to your cluster, you have access to the versions, patches, and over-the-air updates of the Operators and extensions that are published to the catalog.

## About catalogs in OLM 1.0

You can discover installable content by querying a catalog for Kubernetes extensions, such as Operators and controllers, by using the catalogd component. Catalogd is a Kubernetes extension that unpacks catalog content for on-cluster clients and is part of the Operator Lifecycle Manager (OLM) 1.0 suite of microservices. Currently, catalogd unpacks catalog content that is packaged and distributed as container images.

## Operator catalogs in OLM 1.0

Operator Lifecycle Manager (OLM) 1.0 does not include catalogs by default. The following custom resource (CR) example shows how to create a catalog resources for OLM 1.0.

**Example Community Operators catalog**

```yaml
apiVersion: catalogd.operatorframework.io/v1alpha1
kind: Catalog
metadata:
  name: operatorhubio
spec:
  source:
    type: image
    image:
      ref: quay.io/operatorhubio/catalog:latest
```

The following command adds a catalog to your cluster:

**Command syntax**

```terminal
$ kubectl apply -f <catalog_name>.yaml ①
```
1. Specifies the catalog CR, such as `operatorhubio.yaml`.

## About target versions in OLM 1.0

In Operator Lifecycle Manager (OLM) 1.0, cluster administrators set the target version of an Operator declaratively in the Operator’s custom resource (CR).

### Channel specification

If you specify a channel in the Operator’s CR, OLM 1.0 installs the latest release from the specified channel. When updates are published to the specified channel, OLM 1.0 automatically updates to the latest release from the channel.

**Example CR with a specified channel**

```yaml
apiVersion: operators.operatorframework.io/v1alpha1
kind: Operator
metadata:
  name: quay-example
spec:
  packageName: quay-operator
  channel: stable-3.8 ①
```
1. Installs the latest release published to the specified channel. Updates to the channel are automatically installed.

### Target version specification

If you specify the Operator’s target version in the CR, OLM 1.0 installs the specified version. When the target version is specified in the Operator’s CR, OLM 1.0 does not change the target version when updates are published to the catalog.

### Target version updates

If you want to update the version of the Operator that is installed on the cluster, you must manually update the Operator’s CR. Specifying a Operator’s target version pins the Operator’s version to the specified release.

**Example CR with the target version specified**

```yaml
apiVersion: operators.operatorframework.io/v1alpha1
kind: Operator
metadata:
  name: quay-example
spec:
  packageName: quay-operator
  version: 3.8.12 ①
```
1. Specifies the target version. If you want to update the version of the Operator that is installed on the cluster, you must manually update this field the Operator’s CR to the desired target version.

If you want to change the installed version of an Operator, edit the Operator’s CR to the desired target version.

<dl><dt><strong>⚠️ WARNING</strong></dt><dd>

In previous versions of OLM, Operator authors could define upgrade edges to prevent you from updating to unsupported versions. In its current state of development, OLM 1.0 does not enforce upgrade edge definitions. You can specify any version of an Operator, and OLM 1.0 attempts to apply the update.
</dd></dl>

You can inspect an Operator’s catalog contents, including available versions and channels, by running the following command:

**Command syntax**

```terminal
$ oc get package <catalog_name>-<package_name> -o yaml
```

After you create or update a CR, create or configure the Operator by running the following command:

**Command syntax**

```terminal
$ oc apply -f <extension_name>.yaml
```

* If you specify a target version or channel that does not exist, you can run the following command to check the status of your Operator:

  ```terminal
  $ oc get operator.operators.operatorframework.io <operator_name> -o yaml
  ```

  **Example output**

  ```text
  apiVersion: operators.operatorframework.io/v1alpha1
  kind: Operator
  metadata:
    annotations:
      kubectl.kubernetes.io/last-applied-configuration: |
        {"apiVersion":"operators.operatorframework.io/v1alpha1","kind":"Operator","metadata":{"annotations":{},"name":"quay-example"},"spec":{"packageName":"quay-operator","version":"999.99.9"}}
    creationTimestamp: "2023-10-19T18:39:37Z"
    generation: 3
    name: quay-example
    resourceVersion: "51505"
    uid: 2558623b-8689-421c-8ed5-7b14234af166
  spec:
    packageName: quay-operator
    version: 999.99.9
  status:
    conditions:
    - lastTransitionTime: "2023-10-19T18:50:34Z"
      message: package 'quay-operator' at version '999.99.9' not found
      observedGeneration: 3
      reason: ResolutionFailed
      status: "False"
      type: Resolved
    - lastTransitionTime: "2023-10-19T18:50:34Z"
      message: installation has not been attempted as resolution failed
      observedGeneration: 3
      reason: InstallationStatusUnknown
      status: Unknown
      type: Installed
  ```

# Adding a catalog to a cluster

To add a catalog to a cluster, create a catalog custom resource (CR) and apply it to the cluster.

1. Create a catalog custom resource (CR), similar to the following example:

   **Example `redhat-operators.yaml`**

   ```yaml
   apiVersion: catalogd.operatorframework.io/v1alpha1
   kind: Catalog
   metadata:
     name: redhat-operators
   spec:
     source:
       type: image
       image:
         ref: registry.redhat.io/redhat/redhat-operator-index:v{product-version} ①
   ```
   1. Specify the catalog’s image in the `spec.source.image` field.
2. Add the catalog to your cluster by running the following command:

   ```terminal
   $ oc apply -f redhat-operators.yaml
   ```

   **Example output**

   ```text
   catalog.catalogd.operatorframework.io/redhat-operators created
   ```

* Run the following commands to verify the status of your catalog:
  1. Check if you catalog is available by running the following command:

     ```terminal
     $ oc get catalog
     ```

     **Example output**

     ```text
     NAME                  AGE
     redhat-operators      20s
     ```
  2. Check the status of your catalog by running the following command:

     ```terminal
     $ oc get catalogs.catalogd.operatorframework.io -o yaml
     ```

     **Example output**

     ```text
     apiVersion: v1
     items:
     - apiVersion: catalogd.operatorframework.io/v1alpha1
       kind: Catalog
       metadata:
         annotations:
           kubectl.kubernetes.io/last-applied-configuration: |
             {"apiVersion":"catalogd.operatorframework.io/v1alpha1","kind":"Catalog","metadata":{"annotations":{},"name":"redhat-operators"},"spec":{"source":{"image":{"ref":"registry.redhat.io/redhat/redhat-operator-index:v4.14"},"type":"image"}}}
         creationTimestamp: "2023-10-16T13:30:59Z"
         generation: 1
         name: redhat-operators
         resourceVersion: "37304"
         uid: cf00c68c-4312-4e06-aa8a-299f0bbf496b
       spec:
         source:
           image:
             ref: registry.redhat.io/redhat/redhat-operator-index:v{product-version}
           type: image
       status: ①
         conditions:
         - lastTransitionTime: "2023-10-16T13:32:25Z"
           message: successfully unpacked the catalog image "registry.redhat.io/redhat/redhat-operator-index@sha256:bd2f1060253117a627d2f85caa1532ebae1ba63da2a46bdd99e2b2a08035033f" ②
           reason: UnpackSuccessful ③
           status: "True"
           type: Unpacked
         phase: Unpacked ④
         resolvedSource:
           image:
             ref: registry.redhat.io/redhat/redhat-operator-index@sha256:bd2f1060253117a627d2f85caa1532ebae1ba63da2a46bdd99e2b2a08035033f ⑤
           type: image
     kind: List
     metadata:
       resourceVersion: ""
     ```
     1. Stanza describing the status of the catalog.
     2. Output message of the status of the catalog.
     3. Displays the reason the catalog is in the current state.
     4. Displays the phase of the installion process.
     5. Displays the image reference of the catalog.

# Finding Operators to install from a catalog

After you add a catalog to your cluster, you can query the catalog to find Operators and extensions to install.

* You have added a catalog to your cluster.

1. Get a list of the Operators and extensions in the catalog by running the following command:

   ```terminal
   $ oc get packages
   ```

   <details>
   <summary>Example output</summary>

   ```text
   NAME                                                        AGE
   redhat-operators-3scale-operator                            5m27s
   redhat-operators-advanced-cluster-management                5m27s
   redhat-operators-amq-broker-rhel8                           5m27s
   redhat-operators-amq-online                                 5m27s
   redhat-operators-amq-streams                                5m27s
   redhat-operators-amq7-interconnect-operator                 5m27s
   redhat-operators-ansible-automation-platform-operator       5m27s
   redhat-operators-ansible-cloud-addons-operator              5m27s
   redhat-operators-apicast-operator                           5m27s
   redhat-operators-aws-efs-csi-driver-operator                5m27s
   redhat-operators-aws-load-balancer-operator                 5m27s
   ...
   ```
   </details>
2. Inspect the contents of an Operator or extension’s custom resource (CR) by running the following command:

   ```terminal
   $ oc get package <catalog_name>-<package_name> -o yaml
   ```

   **Example command**

   ```text
   $ oc get package redhat-operators-quay-operator -o yaml
   ```

   <details>
   <summary>Example output</summary>

   ```text
   apiVersion: catalogd.operatorframework.io/v1alpha1
   kind: Package
   metadata:
     creationTimestamp: "2023-10-06T01:14:04Z"
     generation: 1
     labels:
       catalog: redhat-operators
     name: redhat-operators-quay-operator
     ownerReferences:
     - apiVersion: catalogd.operatorframework.io/v1alpha1
       blockOwnerDeletion: true
       controller: true
       kind: Catalog
       name: redhat-operators
       uid: 403004b6-54a3-4471-8c90-63419f6a2c3e
     resourceVersion: "45196"
     uid: 252cfe74-936d-44fc-be5d-09a7be7e36f5
   spec:
     catalog:
       name: redhat-operators
     channels:
     - entries:
       - name: quay-operator.v3.4.7
         skips:
         - red-hat-quay.v3.3.4
         - quay-operator.v3.4.6
         - quay-operator.v3.4.5
         - quay-operator.v3.4.4
         - quay-operator.v3.4.3
         - quay-operator.v3.4.2
         - quay-operator.v3.4.1
         - quay-operator.v3.4.0
       name: quay-v3.4
     - entries:
       - name: quay-operator.v3.5.7
         replaces: quay-operator.v3.5.6
         skipRange: '>=3.4.x <3.5.7'
       name: quay-v3.5
     - entries:
       - name: quay-operator.v3.6.0
         skipRange: '>=3.3.x <3.6.0'
       - name: quay-operator.v3.6.1
         replaces: quay-operator.v3.6.0
         skipRange: '>=3.3.x <3.6.1'
       - name: quay-operator.v3.6.10
         replaces: quay-operator.v3.6.9
         skipRange: '>=3.3.x <3.6.10'
       - name: quay-operator.v3.6.2
         replaces: quay-operator.v3.6.1
         skipRange: '>=3.3.x <3.6.2'
       - name: quay-operator.v3.6.4
         replaces: quay-operator.v3.6.2
         skipRange: '>=3.3.x <3.6.4'
       - name: quay-operator.v3.6.5
         replaces: quay-operator.v3.6.4
         skipRange: '>=3.3.x <3.6.5'
       - name: quay-operator.v3.6.6
         replaces: quay-operator.v3.6.5
         skipRange: '>=3.3.x <3.6.6'
       - name: quay-operator.v3.6.7
         replaces: quay-operator.v3.6.6
         skipRange: '>=3.3.x <3.6.7'
       - name: quay-operator.v3.6.8
         replaces: quay-operator.v3.6.7
         skipRange: '>=3.3.x <3.6.8'
       - name: quay-operator.v3.6.9
         replaces: quay-operator.v3.6.8
         skipRange: '>=3.3.x <3.6.9'
       name: stable-3.6
     - entries:
       - name: quay-operator.v3.7.10
         replaces: quay-operator.v3.7.9
         skipRange: '>=3.4.x <3.7.10'
       - name: quay-operator.v3.7.11
         replaces: quay-operator.v3.7.10
         skipRange: '>=3.4.x <3.7.11'
       - name: quay-operator.v3.7.12
         replaces: quay-operator.v3.7.11
         skipRange: '>=3.4.x <3.7.12'
       - name: quay-operator.v3.7.13
         replaces: quay-operator.v3.7.12
         skipRange: '>=3.4.x <3.7.13'
       - name: quay-operator.v3.7.14
         replaces: quay-operator.v3.7.13
         skipRange: '>=3.4.x <3.7.14'
       name: stable-3.7
     - entries:
       - name: quay-operator.v3.8.0
         skipRange: '>=3.5.x <3.8.0'
       - name: quay-operator.v3.8.1
         replaces: quay-operator.v3.8.0
         skipRange: '>=3.5.x <3.8.1'
       - name: quay-operator.v3.8.10
         replaces: quay-operator.v3.8.9
         skipRange: '>=3.5.x <3.8.10'
       - name: quay-operator.v3.8.11
         replaces: quay-operator.v3.8.10
         skipRange: '>=3.5.x <3.8.11'
       - name: quay-operator.v3.8.12
         replaces: quay-operator.v3.8.11
         skipRange: '>=3.5.x <3.8.12'
       - name: quay-operator.v3.8.2
         replaces: quay-operator.v3.8.1
         skipRange: '>=3.5.x <3.8.2'
       - name: quay-operator.v3.8.3
         replaces: quay-operator.v3.8.2
         skipRange: '>=3.5.x <3.8.3'
       - name: quay-operator.v3.8.4
         replaces: quay-operator.v3.8.3
         skipRange: '>=3.5.x <3.8.4'
       - name: quay-operator.v3.8.5
         replaces: quay-operator.v3.8.4
         skipRange: '>=3.5.x <3.8.5'
       - name: quay-operator.v3.8.6
         replaces: quay-operator.v3.8.5
         skipRange: '>=3.5.x <3.8.6'
       - name: quay-operator.v3.8.7
         replaces: quay-operator.v3.8.6
         skipRange: '>=3.5.x <3.8.7'
       - name: quay-operator.v3.8.8
         replaces: quay-operator.v3.8.7
         skipRange: '>=3.5.x <3.8.8'
       - name: quay-operator.v3.8.9
         replaces: quay-operator.v3.8.8
         skipRange: '>=3.5.x <3.8.9'
       name: stable-3.8
     - entries:
       - name: quay-operator.v3.9.0
         skipRange: '>=3.6.x <3.9.0'
       - name: quay-operator.v3.9.1
         replaces: quay-operator.v3.9.0
         skipRange: '>=3.6.x <3.9.1'
       - name: quay-operator.v3.9.2
         replaces: quay-operator.v3.9.1
         skipRange: '>=3.6.x <3.9.2'
       name: stable-3.9
     defaultChannel: stable-3.9
     description: ""
     icon:
       data: PD94bWwgdmVyc2lvbj ...
       mediatype: image/svg+xml
     packageName: quay-operator
   status: {}
   ```
   </details>

# Installing an Operator

You can install an Operator from a catalog by creating an Operator custom resource (CR) and applying it to the cluster.

* You have added a catalog to your cluster.
* You have inspected the details of an Operator to find what version you want to install.

1. Create an Operator CR, similar to the following example:

   **Example `test-operator.yaml` CR**

   ```yaml
   apiVersion: operators.operatorframework.io/v1alpha1
   kind: Operator
   metadata:
     name: quay-example
   spec:
     packageName: quay-operator
     version: 3.8.12
   ```
2. Apply the Operator CR to the cluster by running the following command:

   ```terminal
   $ oc apply -f test-operator.yaml
   ```

   **Example output**

   ```text
   operator.operators.operatorframework.io/quay-example created
   ```

1. View the Operator’s CR in the YAML format by running the following command:

   ```terminal
   $ oc get operator.operators.operatorframework.io/quay-example -o yaml
   ```

   **Example output**

   ```text
   apiVersion: operators.operatorframework.io/v1alpha1
   kind: Operator
   metadata:
     annotations:
       kubectl.kubernetes.io/last-applied-configuration: |
         {"apiVersion":"operators.operatorframework.io/v1alpha1","kind":"Operator","metadata":{"annotations":{},"name":"quay-example"},"spec":{"packageName":"quay-operator","version":"3.8.12"}}
     creationTimestamp: "2023-10-19T18:39:37Z"
     generation: 1
     name: quay-example
     resourceVersion: "45663"
     uid: 2558623b-8689-421c-8ed5-7b14234af166
   spec:
     packageName: quay-operator
     version: 3.8.12
   status:
     conditions:
     - lastTransitionTime: "2023-10-19T18:39:37Z"
       message: resolved to "registry.redhat.io/quay/quay-operator-bundle@sha256:bf26c7679ea1f7b47d2b362642a9234cddb9e366a89708a4ffcbaf4475788dc7"
       observedGeneration: 1
       reason: Success
       status: "True"
       type: Resolved
     - lastTransitionTime: "2023-10-19T18:39:46Z"
       message: installed from "registry.redhat.io/quay/quay-operator-bundle@sha256:bf26c7679ea1f7b47d2b362642a9234cddb9e366a89708a4ffcbaf4475788dc7"
       observedGeneration: 1
       reason: Success
       status: "True"
       type: Installed
     installedBundleResource: registry.redhat.io/quay/quay-operator-bundle@sha256:bf26c7679ea1f7b47d2b362642a9234cddb9e366a89708a4ffcbaf4475788dc7
     resolvedBundleResource: registry.redhat.io/quay/quay-operator-bundle@sha256:bf26c7679ea1f7b47d2b362642a9234cddb9e366a89708a4ffcbaf4475788dc7
   ```
2. Get information about your Operator’s controller manager pod by running the following command:

   ```terminal
   $ oc get pod -n quay-operator-system
   ```

   **Example output**

   ```text
   NAME                                     READY   STATUS    RESTARTS   AGE
   quay-operator.v3.8.12-6677b5c98f-2kdtb   1/1     Running   0          2m28s
   ```

# Updating an Operator

You can update your Operator by manually editing your Operator’s custom resource (CR) and applying the changes.

* You have a catalog installed.
* You have an Operator installed.

1. Inspect your Operator’s package contents to find which channels and versions are available for updating by running the following command:

   ```terminal
   $ oc get package <catalog_name>-<package_name> -o yaml
   ```

   **Example command**

   ```terminal
   $ oc get package redhat-operators-quay-operator -o yaml
   ```
2. Edit your Operator’s CR to update the version to `3.9.1`, as shown in the following example:

   **Example `test-operator.yaml` CR**

   ```yaml
   apiVersion: operators.operatorframework.io/v1alpha1
   kind: Operator
   metadata:
     name: quay-example
   spec:
     packageName: quay-operator
     version: 3.9.1 ①
   ```
   1. Update the version to `3.9.1`
3. Apply the update to the cluster by running the following command:

   ```terminal
   $ oc apply -f test-operator.yaml
   ```

   **Example output**

   ```text
   operator.operators.operatorframework.io/quay-example configured
   ```

   <dl><dt><strong>💡 TIP</strong></dt><dd>

   You can patch and apply the changes to your Operator’s version from the CLI by running the following command:

   ```terminal
   $ oc patch operator.operators.operatorframework.io/quay-example -p \
     '{"spec":{"version":"3.9.1"}}' \
     --type=merge
   ```

   **Example output**

   ```text
   operator.operators.operatorframework.io/quay-example patched
   ```
   </dd></dl>

* Verify that the channel and version updates have been applied by running the following command:

  ```terminal
  $ oc get operator.operators.operatorframework.io/quay-example -o yaml
  ```

  **Example output**

  ```yaml
  apiVersion: operators.operatorframework.io/v1alpha1
  kind: Operator
  metadata:
    annotations:
      kubectl.kubernetes.io/last-applied-configuration: |
        {"apiVersion":"operators.operatorframework.io/v1alpha1","kind":"Operator","metadata":{"annotations":{},"name":"quay-example"},"spec":{"packageName":"quay-operator","version":"3.9.1"}}
    creationTimestamp: "2023-10-19T18:39:37Z"
    generation: 2
    name: quay-example
    resourceVersion: "47423"
    uid: 2558623b-8689-421c-8ed5-7b14234af166
  spec:
    packageName: quay-operator
    version: 3.9.1 ①
  status:
    conditions:
    - lastTransitionTime: "2023-10-19T18:39:37Z"
      message: resolved to "registry.redhat.io/quay/quay-operator-bundle@sha256:4864bc0d5c18a84a5f19e5e664b58d3133a2ac2a309c6b5659ab553f33214b09"
      observedGeneration: 2
      reason: Success
      status: "True"
      type: Resolved
    - lastTransitionTime: "2023-10-19T18:39:46Z"
      message: installed from "registry.redhat.io/quay/quay-operator-bundle@sha256:4864bc0d5c18a84a5f19e5e664b58d3133a2ac2a309c6b5659ab553f33214b09"
      observedGeneration: 2
      reason: Success
      status: "True"
      type: Installed
    installedBundleResource: registry.redhat.io/quay/quay-operator-bundle@sha256:4864bc0d5c18a84a5f19e5e664b58d3133a2ac2a309c6b5659ab553f33214b09
    resolvedBundleResource: registry.redhat.io/quay/quay-operator-bundle@sha256:4864bc0d5c18a84a5f19e5e664b58d3133a2ac2a309c6b5659ab553f33214b09
  ```
  1. Verify that the version is updated to `3.9.1`.

# Deleting an Operator

You can delete an Operator and its custom resource definitions (CRDs) by deleting the Operator’s custom resource (CR).

* You have a catalog installed.
* You have an Operator installed.

* Delete an Operator and its CRDs by running the following command:

  ```terminal
  $ oc delete operator.operators.operatorframework.io quay-example
  ```

  **Example output**

  ```text
  operator.operators.operatorframework.io "quay-example" deleted
  ```

* Run the following commands to verify that your Operator and its resources were deleted:
  * Verify the Operator is deleted by running the following command:

    ```terminal
    $ oc get operator.operators.operatorframework.io
    ```

    **Example output**

    ```text
    No resources found
    ```
  * Verify that the Operator’s system namespace is deleted by running the following command:

    ```terminal
    $ oc get ns quay-operator-system
    ```

    **Example output**

    ```text
    Error from server (NotFound): namespaces "quay-operator-system" not found
    ```
