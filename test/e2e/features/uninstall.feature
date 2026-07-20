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

