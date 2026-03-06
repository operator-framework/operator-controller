Feature: Uninstall ClusterExtension

  As an OLM user I would like to uninstall a cluster extension,
  removing all resources previously installed/updated through the extension.

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles
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
            packageName: test
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    And bundle "test-operator.1.2.0" is installed in version "1.2.0"
    And ClusterExtension is rolled out
    And ClusterExtension resources are created and labeled

  Scenario: Removing ClusterExtension triggers the extension uninstall, eventually removing all installed resources
    When ClusterExtension is removed
    Then the ClusterExtension's constituent resources are removed
