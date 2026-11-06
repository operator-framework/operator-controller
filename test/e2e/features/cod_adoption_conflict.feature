@BoxcutterRuntime
Feature: ClusterExtension only adopts a ClusterObjectDeployment that belongs to it

  As an OLM user, I want a ClusterExtension to report and retry an adoption conflict,
  so that it does not take over a foreign or unlabelled ClusterObjectDeployment
  and can proceed after that deployment is removed or relabelled.

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

  Scenario: ClusterExtension retries after an ownerless unlabelled ClusterObjectDeployment is relabelled
    Given resource is applied
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
    And resource "clusterobjectdeployment/${NAME}" UID is remembered
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
      ClusterObjectDeployment "${NAME}" has no controller and does not have the required apps.kubernetes.io/managed-by=operator-controller label
      """
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
        labels:
          apps.kubernetes.io/managed-by: operator-controller
      spec:
        template:
          spec:
            collisionProtection: Prevent
            phases:
            - name: resources
              objects: []
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterObjectDeployment "${NAME}" is owned by ClusterExtension "${NAME}"
    And resource "clusterobjectdeployment/${NAME}" keeps its UID
