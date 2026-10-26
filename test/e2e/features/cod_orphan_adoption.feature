@BoxcutterRuntime
Feature: ClusterObjectDeployment adopts an orphan ClusterObjectSet

  As an OLM user applying a ClusterObjectDeployment after a migration has created
  an orphan ClusterObjectSet (no controller owner, no template-hash label) in the
  same group, I want the ClusterObjectDeployment to adopt that ClusterObjectSet
  and roll the next revision because the adopted orphan lacks the template-hash label.

  Background:
    Given OLM is available

  Scenario: A ClusterObjectDeployment adopts a pre-existing orphan ClusterObjectSet and rolls the next revision
    Given namespace "${TEST_NAMESPACE}" is available
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${NAME}-1
      spec:
        group: ${NAME}
        revision: 1
        lifecycleState: Active
        collisionProtection: None
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
    And ClusterObjectSet "${NAME}-1" has group "${NAME}"
    And ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
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
    Then ClusterObjectSet "${NAME}-1" is controller-owned by ClusterObjectDeployment "${NAME}"
    And ClusterObjectSet "${NAME}-2" is controller-owned by ClusterObjectDeployment "${NAME}"
    And ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${NAME}-1" is archived
    And ClusterObjectDeployment "${NAME}" reports Available as True with Reason Available
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed
