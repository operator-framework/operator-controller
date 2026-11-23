Feature: Operator upgrade verification

  As an OLM developer I would like to verify that after upgrading OLM itself,
  pre-existing ClusterCatalogs and ClusterExtensions continue to function
  and can be updated.

  Scenario: Pre-existing catalog and extension survive OLM upgrade and extension update
    Given the latest stable OLM release is installed
    And an image registry is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.0.0   | beta    |          | CRD, Deployment, ConfigMap |
      | test    | 1.0.1   | beta    | 1.0.0    | CRD, Deployment, ConfigMap |
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
            version: 1.0.0
      """
    And ClusterExtension is available
    And ClusterExtension is rolled out
    And bundle "${PACKAGE:test}.1.0.0" is installed in version "1.0.0"
    And resource "clusterextension/${NAME}" UID is remembered
    And Helm-backed extension resource identities are remembered
    When OLM is upgraded
    And catalogd is ready to reconcile resources
    And operator-controller is ready to reconcile resources

    And catalog "test" is reconciled
    Then catalog "test" reports Progressing as True with Reason Succeeded
    And catalog "test" reports Serving as True with Reason Available

    And pre-existing ClusterExtension storage has transitioned after OLM upgrade
    And ClusterExtension is available
    And ClusterExtension is rolled out
    And bundle "${PACKAGE:test}.1.0.0" is installed in version "1.0.0"
    And resource "clusterextension/${NAME}" keeps its UID
    And extension resources keep their pre-upgrade identities
    When ClusterExtension version is updated to "1.0.1"
    Then ClusterExtension is available
    And ClusterExtension is rolled out
    And bundle "${PACKAGE:test}.1.0.1" is installed in version "1.0.1"
    And deployment "test-operator-${SCENARIO_ID}" rollout is complete
    And updated ClusterExtension storage is healthy
    And resource "clusterextension/${NAME}" keeps its UID
    And retained extension resources keep their pre-upgrade identities
