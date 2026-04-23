Feature: Uninstall ClusterExtension

  As an OLM user I would like to uninstall a cluster extension,
  removing all resources previously installed/updated through the extension.

  Background:
    Given OLM is available
    And an image registry is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
    And ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
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

  Scenario: Removing ClusterExtension resources leads to all installed resources being removed even if the service account is no longer present
    When resource "serviceaccount/olm-sa" is removed
    # Ensure service account is gone before checking to ensure resources are cleaned up whether the service account
    # and its permissions are present on the cluster or not
    And resource "serviceaccount/olm-sa" is eventually not found
    And ClusterExtension is removed
    Then the ClusterExtension's constituent resources are removed
