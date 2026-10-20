@BoxcutterRuntime
Feature: ClusterObjectDeployment revision rollout

  As an OLM user applying a ClusterObjectDeployment, I want the controller to roll
  out my template as an immutable ClusterObjectSet revision, create a new revision
  when the template changes, and report rollout progress on the Progressing
  condition.

  Background:
    Given OLM is available

  Scenario: ClusterObjectDeployment rolls out its initial revision
    Given namespace "${TEST_NAMESPACE}" is available
    When resource is applied
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
              objects:
              - object:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    Then ClusterObjectSet "${NAME}-1" has group "${NAME}"
    And ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed

  Scenario: Updating the template creates a new revision
    Given namespace "${TEST_NAMESPACE}" is available
    When resource is applied
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
              objects:
              - object:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    And ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And resource is applied
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
              objects:
              - object:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
              - object:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-cm-2
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    Then ClusterObjectSet "${NAME}-2" has group "${NAME}"
    And ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed
