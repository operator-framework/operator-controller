@BoxcutterRuntime
Feature: Track ClusterObjectSet revisions by group

  Background:
    Given OLM is available
    And an image registry is available
    And namespace "${TEST_NAMESPACE}" is available

  Scenario: Revision pruning is isolated by group
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.0.0   | beta    |          | CRD, Deployment, ConfigMap |
      | test    | 1.0.1   | beta    | 1.0.0    | CRD, Deployment, ConfigMap |
      | test    | 1.0.2   | beta    | 1.0.1    | CRD, Deployment, ConfigMap |
      | test    | 1.0.3   | beta    | 1.0.2    | CRD, Deployment, ConfigMap |
      | test    | 1.0.4   | beta    | 1.0.3    | CRD, Deployment, ConfigMap |
      | test    | 1.0.5   | beta    | 1.0.4    | CRD, Deployment, ConfigMap |
      | test    | 1.0.6   | beta    | 1.0.5    | CRD, Deployment, ConfigMap |
    And ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
        labels:
          olm.operatorframework.io/owner-name: ${NAME}
      spec:
        group: ${NAME}-other
        revision: 1
        lifecycleState: Archived
        collisionProtection: Prevent
      """
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
            version: 1.0.0
            selector:
              matchLabels:
                olm.operatorframework.io/metadata.name: ${CATALOG:test}
      """
    And ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.1"
    Then ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.2"
    Then ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.3"
    Then ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.4"
    Then ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.5"
    Then ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.6"
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterExtension reports "${NAME}-7" as active revision
    And ClusterObjectSet "${NAME}-7" has group "${NAME}"
    And resource "clusterobjectset/${NAME}-1" is eventually not found
    # The existing retention policy keeps five prior revisions plus the current one.
    And ClusterExtension "${NAME}" owns 6 ClusterObjectSets
    And resource "clusterobjectset/${COS_NAME}" exists
    And ClusterObjectSet "${COS_NAME}" has group "${NAME}-other"
