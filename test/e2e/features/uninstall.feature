Feature: Uninstall ClusterExtension

  As an OLM user I would like to uninstall a previously installed extension

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles
    And ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}
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
            packageName: test
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "test-operator.1.2.0" is installed in version "1.2.0"
    And resource "networkpolicy/test-operator-network-policy" is installed
    And resource "configmap/test-configmap" is installed
    And resource "deployment/test-operator" is installed
    When ClusterExtension "${NAME}" is deleted
    Then resource "networkpolicy/test-operator-network-policy" is not found
    And resource "configmap/test-configmap" is not found
    And resource "deployment/test-operator" is not found
