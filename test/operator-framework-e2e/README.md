# Cross-component E2E for operator framework

This is a cross-component demo with all OLM v1 repositories. The ginkgo test does the following: 
-  Automates the creation of `plain+v0` bundles and FBCs for a set of bundle manifest directories.
- Installs, upgrades and deletes a `plain+v0` operator.
- Uses operator-sdk to build `registry+v1` bundles and create catalogs to include the bundles.
- Installs, upgrades and deletes a `registry+v1` operator.

## Objective
- Development on OLM v1 is split across multiple repositories, and the list of relevant repositories may grow over time. While we have demos showing improvements in functionality of components over time, it can be difficult to have a picture of the state of OLM v1 at any given time for someone not following its development closely. Having a single source to look for OLM v1 behavior can provide more clarity about the state of the project.
- With the scale of the OLM v1 project, it is useful to have a means to test components in the operator development + lifecycle pipeline together to create a more cohesive experience for all users.

## Getting Started

- Building operator-controller, deploying it into the cluster and rest of the configuration is done in the `MakeFile` of this repo under the target `operator-developer-e2e`. This includes:
    
    - Setting up a kind cluster.
    - Installing the operator controller onto the cluster.
    - Downloading the opm tool.
    - Installing the operator-sdk.
    - Setting up a local registry server for building and loading images.

- The following structs defined are required as input for both plain+v0 and registry+v1 bundles:
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
        - `baseFolderPath` - Base path of the folder for the specific bundle type input data.
        - `bundles` - Stores the data relevant to different versions of the bundle.
        - `bInputDir` - The input directory containing the specific version of the bundle data.
        - `bundleVersion` - The specific version of the bundle data.
        - `imageRef` - This is formed. Stores the bundle image reference which will be of the format `localhost:5000/<operator_name>-bundle:v.<bundleVersion>`
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
        - `baseFolderPath` - Base path of the folder for the catalogs.
        - `operatorName` - Name of the operator to be installed from the bundles.
        - `desiredChannelName` - Desired channel name for the operator.
        - `catalogDir` - This is formed. The directory to store the FBC. The formed value will be of the format: `<operator-name>-catalog`
        - `imageRef` - This is formed. Stores the FBC image reference which will be of the format: `localhost:5000/<username>/<catalogDir>:test`
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

### Plain bundles
- Plain bundle manifests are taken as input.

    - The plain bundle manifest directory taken as input should follow the below directory structure:
        ```
            bundles/
                └── plain-v0/
                    ├── plain.v<version>/
                    │        ├── manifests
                    │        └── Dockerfile
                    └── plain.v<version>/
                                ├── manifests
                                └── Dockerfile
        ```
    - The bundles should be present in the testdata folder.

- After the bundle image is created and loaded, the FBC is formed by a custom routine by using the operatorName, desiredChannelName, bundle imageRefs and bundleVersions.
   
   - The generated FBC will be present in the file path: `testdata/catalogs/<operatorName>-catalog/catalog.yaml` and the Dockerfile will be formed in the file path: `testdata/catalogs/<operatorName>-catalog.Dockerfile`
   - `Default channel` is not used in forming the FBC as it is not an OLMv1 concept.
   - The package schema name will be the operatorName and the bundle name will be `<operatorName>.v<version>`.

- The catalog resource is then formed with the name `<operatorName>-catalog`.

- The operator is then installed and has the name `<operatorName>`.

### Registry Bundles

- The registry+v1 bundles are formed using operator-sdk.
    - The below input is used to form the bundle using operator-sdk.
        ```
        type SdkProjectInfo struct {
            projectName string
            domainName  string
            group       string
            version     string
            kind        string
        }
        ```
    - The generated CSV uses the default values. 
    - The bundle content is formed in the operator-sdk project directory in the folder `bundle`. This is moved to the folder path: `testdata/bundles/registry-v1/<operatorName>.v<version>`.
    - The generated Dockerfile has the name `bundle.Dockerfile`.
    - The same project directory needs to be updated for forming a different version of the `bundle`.
- After the bundle image is created and loaded, the FBC is formed using `semver` and `opm` tool. 
    - The semver named `registry-semver.yaml` is formed by passing the bundle imageRefs.
    - The generated FBC will be present in the file path: `testdata/catalogs/<operatorName>-catalog/catalog.yaml` and the Dockerfile will be formed in the file path: `testdata/catalogs/<operatorName>-catalog.Dockerfile`
    - The package schema name will be the operator-sdk projectName.

- The catalog resource is then formed with the name `<operatorName>-catalog`.

- The operator is then installed and has the name `<operatorName>`.


- After the e2e workflow, all the files formed are cleared and the catalog is deleted.
