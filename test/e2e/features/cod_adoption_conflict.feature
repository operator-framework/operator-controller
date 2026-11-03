@BoxcutterRuntime
Feature: ClusterExtension refuses to adopt a ClusterObjectDeployment owned by another resource

  As an OLM user, I want a ClusterExtension to report and retry an adoption conflict,
  so that it does not take over a ClusterObjectDeployment controlled by another owner
  and can proceed after that deployment is removed.

  Background:
    Given OLM is available
    And an image registry is available
    And namespace "${TEST_NAMESPACE}" is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |

  Scenario: ClusterExtension retries after a foreign-owned ClusterObjectDeployment is removed
    # Note: we're using a namespace owner in lieu of an existing controller to simulate foreign controller ownership
    Given ClusterObjectDeployment with Namespace owner "${TEST_NAMESPACE}" is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
      spec:
        template:
          spec:
            collisionProtection: Prevent
            phases:
            - name: resources
              objects: []
      """
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
            version: 1.2.0
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      ClusterObjectDeployment "${NAME}" is already controlled by Namespace "${TEST_NAMESPACE}"
      """
    When resource "clusterobjectdeployment/${NAME}" is removed
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterObjectDeployment "${NAME}" is owned by ClusterExtension "${NAME}"
