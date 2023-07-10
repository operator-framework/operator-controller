# Cross-component E2E for operator framework

This is a cross-component demo with all OLM v1 repositories. The ginkgo test does the following: 
-  Automates the creation of plain+v0 bundles and FBCs for a set of bundle manifest directories.
- Creates, upgrades and deletes a plain+v0 operator.

## Objective
- Development on OLM v1 is split across multiple repositories, and the list of relevant repositories may grow over time. While we have demos showing improvements in functionality of components over time, it can be difficult to have a picture of the state of OLM v1 at any given time for someone not following its development closely. Having a single source to look for OLM v1 behavior can provide more clarity about the state of the project.
- With the scale of the OLM v1 project, it is useful to have a means to test components in the operator development + lifecycle pipeline together to create a more cohesive experience for all users.

## Getting Started

- Plain bundle manifests are taken as input.

    - The plain bundle manifest directory taken as input should follow the below directory structure:
        ```
            bundles/
                └── plain-v0/
                    ├── plain.v0.1.0/
                    │        ├── manifests
                    │        └── Dockerfile
                    └── plain.v0.1.1/
                                ├── manifests
                                └── Dockerfile
        ```
    - The bundles should present in the testdata folder.
    - The bundle paths and bundle image reference is hard coded in the test.

- After the bundle image is created and loaded, the FBC is created by accepting the required information in a config file named `catalog_config.yaml` in the directory `test/operator-e2e/config`.

    - Example `catalog_config.yaml` file:
        ```
        schema: catalog-config
        packageName: plain
        channelData:
        - channelName: foo
            channelEntries:
            - entryVersion: 0.1.0
            replaces: null
            skipRange: null
            skips:
            - null
        bundleData:
        - bundleImage: foobar:v0.1.0
            bundleVersion: 0.1.0
        ```
    - The generated FBC will be present in the directory structure: `testdata/catalogs/plainv0-test-catalog`.
    - The Dockerfile is generated and the FBC image is built and loaded.
    - The output catalog path, catalog name, catalog image reference are hard-coded in the test.
    - The FBC generated is validated using opm.

- The FBC image is then used to create an operator.
    -  Based on the catalog data, we hard code the package names and their version that is to be installed and upgraded.
