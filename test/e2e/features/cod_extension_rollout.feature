@BoxcutterRuntime
Feature: ClusterExtension rollout via ClusterObjectDeployment

  As an OLM user installing a ClusterExtension, I want the operator-controller to
  create a ClusterObjectDeployment that manages the rollout lifecycle, creating
  ClusterObjectSet revisions for the installed bundle, so that extensions benefit
  from the full COD/COS revision management system.

  Background:
    Given OLM is available
    And an image registry is available
    And namespace "${TEST_NAMESPACE}" is available

  Scenario: ClusterExtension creates a ClusterObjectDeployment and installs via ClusterObjectSet
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    When ClusterExtension is applied
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
    Then resource "clusterobjectdeployment/${NAME}" is available
    And ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And ClusterExtension is available
    And ClusterExtension reports Installed as True
    And bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"
