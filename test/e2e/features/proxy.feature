Feature: HTTP Proxy Support for Operator Deployments

  As an OLM user I would like operators installed from catalogs to inherit
  HTTP proxy configuration from the operator-controller, enabling them to
  function correctly in environments that require HTTP proxies for outbound
  connections.

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles
    And ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}

  Scenario: Operator deployment has no proxy env vars in default configuration
    When ClusterExtension is applied
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
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And resource "deployment/test-operator" is installed
    And resource "deployment/test-operator" has no proxy environment variables
    When ClusterExtension is removed
    Then the ClusterExtension's constituent resources are removed

  Scenario: Operator deployment inherits proxy env vars from operator-controller
    Given operator-controller has environment variable "NO_PROXY" set to "localhost,.cluster.local"
    When ClusterExtension is applied
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
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And resource "deployment/test-operator" is installed
    And resource "deployment/test-operator" has environment variable "NO_PROXY" set to "localhost,.cluster.local"
    When operator-controller has environment variable "NO_PROXY" removed
    When ClusterExtension is removed
    Then the ClusterExtension's constituent resources are removed

  Scenario: Operator deployment inherits proxy env vars and updates when they are removed
    Given operator-controller has environment variable "NO_PROXY" set to "localhost,.cluster.local"
    When ClusterExtension is applied
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
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And resource "deployment/test-operator" is installed
    And resource "deployment/test-operator" has environment variable "NO_PROXY" set to "localhost,.cluster.local"
    When operator-controller has environment variable "NO_PROXY" removed
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterExtension revision manifests have no proxy environment variables
    And resource "deployment/test-operator" has no proxy environment variables
    When ClusterExtension is removed
    Then the ClusterExtension's constituent resources are removed
