Feature: Uninstall ClusterExtension

  As an OLM user I would like to uninstall a cluster extension,
  removing all resources previously installed/updated through the extension.

  Background:
    Given OLM is available
    And an image registry is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    And namespace "${TEST_NAMESPACE}" is available
    And ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"
    And ClusterExtension is rolled out
    And ClusterExtension resources are created and labeled

  Scenario: Removing ClusterExtension triggers the extension uninstall, eventually removing all installed resources
    When ClusterExtension is removed
    Then the ClusterExtension's constituent resources are removed

  @BoxcutterRuntime
  Scenario: Removing ClusterExtension cascades to grouped revisions and their content Secrets
    Given ClusterObjectSet "${NAME}-1" has group "${NAME}"
    And ClusterExtension "${NAME}" owns 1 ClusterObjectSet
    And ClusterObjectSet "${NAME}-1" referred secrets are owned by the object set
    And ClusterObjectSet "${NAME}-1" referred secrets are remembered
    When ClusterExtension is removed
    Then resource "clusterobjectset/${NAME}-1" is eventually not found
    And the ClusterExtension's constituent resources are removed
    And the remembered revision secrets are removed
