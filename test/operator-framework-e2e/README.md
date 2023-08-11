# Cross-component E2E for operator framework

This is a cross-component demo with all OLM v1 repositories. The ginkgo test does the following: 
-  Uses operator-sdk and kustomize to build `plain+v0` bundles and create catalogs to include the bundles.
- Installs, upgrades and deletes a `plain+v0` operator.
- Uses operator-sdk to build `registry+v1` bundles and create catalogs to include the bundles.
- Installs, upgrades and deletes a `registry+v1` operator.

The steps in the ginkgo test can be summarized as follows:

1. start with an empty directory
2. call operator-sdk to initialize and generate an operator
3. generate a bundle directory
4. build/push/kind load bundle images from the bundle directories
5. repeat steps 2-4 as necessary to get bundles for multiple operator versions
6. generate a catalog directory
7. build/push/kind load the catalog
8. create a Catalog CR (with kubectl operator)
9. create an Operator CR (with kubectl operator)
10. trigger Operator upgrades (with kubectl operator)
11. delete the Operator CR (with kubectl operator)
12. delete the Catalog CR (with kubectl operator)
13. repeat steps 2-12 for each bundle format (e.g. registry+v1 and plain+v0)
## Objective
- Development on OLM v1 is split across multiple repositories, and the list of relevant repositories may grow over time. While we have demos showing improvements in functionality of components over time, it can be difficult to have a picture of the state of OLM v1 at any given time for someone not following its development closely. Having a single source to look for OLM v1 behavior can provide more clarity about the state of the project.
- With the scale of the OLM v1 project, it is useful to have a means to test components in the operator development + lifecycle pipeline together to create a more cohesive experience for all users.

## Getting Started
- This test currently only works with the container runtime `docker`.
- Building operator-controller, deploying it into the cluster and rest of the configuration is done in the `MakeFile` of this repo under the target `operator-developer-e2e`. This includes:
    
    - Setting up a kind cluster named `operator-controller-op-dev-e2e`.
    - Installing the operator controller onto the cluster.
    - Setting up `opm`, `operator-sdk` and `kustomize` using bingo.
    - Setting up a local registry server for building and loading images.
### Input Values used

Below are the input values used in the test which is specified in the `operator_framework_test.go`.

- The following structs defined are required, to accept input for both `plain+v0` and `registry+v1` bundles:
   - For getting bundle related inputs:
        ```
        type BundleInfo struct {
            baseFolderPath string 
            bundles        []BundleContent
        }

        type BundleContent struct {
            bInputDir     string
            bundleVersion string
            imageRef      string
        }
        ```
        - `baseFolderPath` - Base/root path of the folder where the specific bundle type input data is stored.[root path to plain-v0 or registry-v1 bundles testdata]
        - `bundles` - Stores the data relevant to different versions of the bundle.
        - `bInputDir` - The directory that stores the specific version of the bundle data. The name of the directory is formed and is of the format `<operatorName>.v<bundleVersion>`.
        - `bundleVersion` - The specific version of the bundle data.
        - `imageRef` - This is formed. Stores the bundle image reference which will be of the format `<registry_repo>/<   operatorName>-bundle:v.<bundleVersion>`
    - For getting catalog related inputs:
        ```
        type CatalogDInfo struct {
            baseFolderPath     string
            catalogDir         string
            operatorName       string
            desiredChannelName string
            imageRef           string
            fbcFileName        string
        }
        ```
        - `baseFolderPath` - Base/root path of the folder that stores the catalogs.
        - `operatorName` - Name of the operator to be installed from the bundles.
        - `desiredChannelName` - Desired channel name for the operator.
        - `catalogDir` - This is formed. The directory to store the catalog/FBC. The directory name will be of the format: `<operator-name>-catalog`
        - `imageRef` - This is formed. Stores the FBC image reference which will be of the format: `<registry_repo>/<catalogDir>:test`
        - `fbcFileName` - Name of the FBC file. This is hard-coded as `catalog.yaml`.
    - For getting information related to the install/upgrade action for operators:
        ```
        type OperatorActionInfo struct {
            installVersion string 
            upgradeVersion string
        }
        ``` 
        - `installVersion` - Version of the operator to be installed on the cluster.
        - `upgradeVersion` - Version of the operator to be upgraded on the cluster.

    -   The below inputs are used to form the bundle using operator-sdk.

        ```
        type SdkProjectInfo struct {
            projectName string
            domainName  string
            group       string
            version     string
            kind        string
        }
        ```
## How to run
- Makefile target `operator-developer-e2e` : Runs the entire E2E setup.
- Makefile target `test-op-dev-e2e`: Runs the ginkgo test.
- Makefile target `deploy-local-registry`: Deploys local registry
- Makefile target `cleanup-local-registry` : Stops and removes local registry
- Makefile target `kind-cluster-cleanup`: Deletes the kind cluster

## Bundle Types
### Plain bundles
- The `plain+v0` bundles are formed using `operator-sdk` and `kustomize`. 
    - The `kustomize` organizes the different resources present in the `operator-sdk` project into a single yaml file.
    - The Dockerfile for the bundle is named `plainbundle.Dockerfile` and is generated using a custom routine.
    - The generated bundle is stored in the format:
        ```
        plain-v0
          └── <operatorName>.v<bundleVersion>
                └── manifests
                │      └── mainfest.yaml
                └── plainbundle.Dockerfile
        ```


- The FBC template is formed by a custom routine by using the operatorName, desiredChannelName, bundle imageRefs and bundleVersions.
   - `Default channel` is not used in forming the FBC as it is not an OLMv1 concept.
   - Only one `olm.channel` is generated which is the given <desiredChannelName>.
   - Upgrade graph is formed with only replaces edge.
   - The generated FBC is not validated using `opm` as the tool has no support for plain bundles.
   - The Dockerfile for the catalog is named `<operator-name>-catalog.Dockerfile` and is generated using a custom routine.
   - The generated catalog is stored in the format:
        ```
        catalogs
          └── <operator-name>-catalog
          │     └── catalog.yaml
          └── <operator-name>-catalog.Dockerfile
        ```
- The catalog CR is then formed with the name `<operatorName>-catalog`.

- The operator is then installed and has the name `<operatorName>`.

### Registry Bundles

- The registry+v1 bundles are formed using operator-sdk.
    - The generated CSV uses the default values. 
    - The bundle content is formed within the operator-sdk project directory in the folder `bundle`. This is moved to the bundle directory folder.
    - The generated Dockerfile has the name `bundle.Dockerfile`. The Dockerfile and bundle structure is genearted by the `operator-sdk` tool.
    - The generated bundle is stored in the format:
        ```
        registry-v1
          └── <operatorName>.v<bundleVersion>
                └── manifests    
                └── metadata
                └── bundle.Dockerfile
        ```

- The FBC is formed using `opm alpha render-template semver` tool. 
    - The semver template named `registry-semver.yaml` is formed using a custom routine by passing the bundle imageRefs.
    - `generatemajorchannels` and `generateminorchannels` is set to false in the semver template.
    - The generated catalog is stored in the format:
        ```
        catalogs
            └── <operator-name>-catalog
            │     └── catalog.yaml
            └── <operator-name>-catalog.Dockerfile
        ```

- The catalog resource is then formed with the name `<operatorName>-catalog`.

- The operator is then installed and has the name `<operatorName>`.


- After the e2e workflow, all the files formed are cleared.


## To-do
1. The resources are read from input manifests using universal decoder from `k8s.io/apimachinery/pkg/runtime/serializer`. 
    -  However, in cases where a single file contains multiple YAML documents separated by `---,` the `UniversalDecoder` recognizes only the first resource. This situation is relevant as for `plain+v0` bundles generated through `kustomize,` the manifest has multiple YAML documents are combined into one file using --- separators. This is now handled by splitting the content of the YAML file and decoding each of them using the `UniversalDecoder`. 
    - This workaround can be improved using `YAMLToJSONDecoder` from `k8s.io/apimachinery/pkg/util/yaml`. And the kind, api version and name can be get by decoding into `Unstructured` from `k8s.io/apimachinery/pkg/apis/meta/v1/unstructured`.
2. All the tests pass and the operator is installed successfully. The bundledeployment succeeds and the resources are created. But the pod for the new operator failes due to `ImagePullBackOff`. 
    - This is because the  `Deployment` controller-manager uses the image `controller:latest` which is not present on the cluster.
    - The solution would be to replace `controller:latest` with the `busybox:latest` and then pulling and loading  `busybox:latest` onto cluster.
    - The replacement could possibly be achieved by adding the following to `config/default/kustomization.yaml` under `operator-sdk` project:
        ```
        images:
        - name: controller
        newName: controller
        newTag: latest
        ```
## Issues
1. This test currently only works with the container runtime `docker`. 
    - The default value of CONTAINER_RUNTIME defined in the Makefile is `docker`. Therefore the correct runtime has to be assigned to the variable `CONTAINER_RUNTIME` before calling the make target if docker is what is not being used. The test routine also assumes the runtime as `docker` if it is unable to retrieve the value of the environment variable.
    - But this is only a partial fix to the problem. With this change the test for `plain+v0` bundles will pass but for `registry+v1` will fail for other container runtimes. This is because `registry+v1` uses `operator-sdk` support. Thus to mimic the user experience, the targets `bundle-build` and `bundle-push` from the generated Makefile by operator-sdk tool, which has docker being hard coded as the container runtime, is used to build and push the bundle images. This could be marked as an issue and addressed when hard coding docker as container runtime in the generated Makefile is addressed by operator-sdk.

2. The `opm`,`operator-sdk` and `kustomize` binaries are added in operator-controller using `bingo`.
    -  But based on discussions, if required test should be changed so that it has `opm` and `operator-sdk` in `PATH` and simply runs it like `exec.Command("opm", ...)`.
    - This will enable in running [a matrix](https://docs.github.com/en/actions/using-jobs/using-a-matrix-for-your-jobs) for the tests and to use different versions of `opm` and `operator-sdk`.
    - This might help in emulating the user experience better.

## Tooling gaps

Following are the tooling gaps identified while testing `operator-framework` end-to-end:
- `opm` doesn't have plain bundle support.
- No tool for forming FBC for plain bundles.
- No tool for generating Dockerfile for plain bundles.
- No tool for generating Dockerfile for plain catalogs.
- Since `opm` doesn't have plain bundle support, there is no means to validate the FBC generated for plain bundles.
 
