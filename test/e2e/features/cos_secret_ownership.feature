@BoxcutterRuntime
Feature: ClusterObjectSet and ClusterObjectDeployment Secret Ownership and Garbage Collection

  As an OLM user, when I delete a ClusterObjectSet or ClusterObjectDeployment,
  I expect its referenced Secrets to be garbage-collected automatically.

  Cleanup is only guaranteed once the ClusterObjectSet has taken ownership of a
  referenced Secret (added its owner reference). Until that point, the Secret's
  lifecycle is the responsibility of whoever created it.

  Background:
    Given OLM is available
    And namespace "${TEST_NAMESPACE}" is available

  Scenario: Deleting a standalone ClusterObjectSet garbage-collects its object-data Secrets
    When resource is applied
      """
      apiVersion: v1
      kind: Secret
      metadata:
        name: ${COS_NAME}-secret
        namespace: ${TEST_NAMESPACE}
      immutable: true
      type: olm.operatorframework.io/object-data
      stringData:
        configmap: |
          {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
              "name": "test-cm",
              "namespace": "${TEST_NAMESPACE}"
            },
            "data": {
              "key": "value"
            }
          }
      """
    And ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
      spec:
        group: ${NAME}
        lifecycleState: Active
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - ref:
              name: ${COS_NAME}-secret
              namespace: ${TEST_NAMESPACE}
              key: configmap
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${COS_NAME}" referred secrets have a non-controller owner reference to the object set
    And ClusterObjectSet "${COS_NAME}" referred secrets are remembered
    When resource "clusterobjectset/${COS_NAME}" is removed
    Then resource "clusterobjectset/${COS_NAME}" is eventually not found
    And the remembered revision secrets are removed

  Scenario: Deleting a ClusterObjectDeployment garbage-collects its revisions' object-data Secrets
    When resource is applied
      """
      apiVersion: v1
      kind: Secret
      metadata:
        name: ${NAME}-cod-secret
        namespace: ${TEST_NAMESPACE}
      immutable: true
      type: olm.operatorframework.io/object-data
      stringData:
        configmap: |
          {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
              "name": "test-cm-cod",
              "namespace": "${TEST_NAMESPACE}"
            },
            "data": {
              "key": "value"
            }
          }
      """
    And resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}-cod
      spec:
        template:
          spec:
            collisionProtection: Prevent
            phases:
            - name: resources
              objects:
              - ref:
                  name: ${NAME}-cod-secret
                  namespace: ${TEST_NAMESPACE}
                  key: configmap
      """
    Then ClusterObjectDeployment "${NAME}-cod" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectDeployment "${NAME}-cod" reports Available as True with Reason Available
    And ClusterObjectDeployment "${NAME}-cod" revision secrets are remembered
    When resource "clusterobjectdeployment/${NAME}-cod" is removed
    Then resource "clusterobjectdeployment/${NAME}-cod" is eventually not found
    And the remembered revision secrets are removed

  Scenario: A referenced immutable Opaque Secret is adopted and garbage-collected
    When resource is applied
      """
      apiVersion: v1
      kind: Secret
      metadata:
        name: ${COS_NAME}-opaque-secret
        namespace: ${TEST_NAMESPACE}
      immutable: true
      type: Opaque
      stringData:
        configmap: |
          {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
              "name": "test-cm-opaque",
              "namespace": "${TEST_NAMESPACE}"
            },
            "data": {
              "key": "value"
            }
          }
      """
    And ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
      spec:
        group: ${NAME}
        lifecycleState: Active
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - ref:
              name: ${COS_NAME}-opaque-secret
              namespace: ${TEST_NAMESPACE}
              key: configmap
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${COS_NAME}" referred secrets have a non-controller owner reference to the object set
    And ClusterObjectSet "${COS_NAME}" referred secrets are remembered
    When resource "clusterobjectset/${COS_NAME}" is removed
    Then resource "clusterobjectset/${COS_NAME}" is eventually not found
    And the remembered revision secrets are removed
