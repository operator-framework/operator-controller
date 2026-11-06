@BoxcutterRuntime
Feature: ClusterObjectDeployment revision history limit

  As an OLM user applying a ClusterObjectDeployment, I want archived (superseded)
  ClusterObjectSet revisions beyond spec.revisionHistoryLimit to be garbage
  collected, so that rollout history is bounded while recent revisions remain
  available for inspection and rollback.

  Background:
    Given OLM is available

  Scenario: Archived revisions beyond the revisionHistoryLimit are garbage collected
    Given namespace "${TEST_NAMESPACE}" is available
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
      spec:
        revisionHistoryLimit: 1
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
                    k: v1
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    # Second revision supersedes and archives the first.
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
      spec:
        revisionHistoryLimit: 1
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
                    k: v2
      """
    Then ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${NAME}-1" is archived
    # Third revision supersedes and archives the second. With revisionHistoryLimit=1
    # there are now two archived revisions (1 and 2), so the oldest (1) is pruned
    # while the newest archived revision (2) is retained.
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
      spec:
        revisionHistoryLimit: 1
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
                    k: v3
      """
    Then ClusterObjectSet "${NAME}-3" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${NAME}-2" is archived
    And ClusterObjectSet "${NAME}-1" is eventually deleted
    And ClusterObjectDeployment "${NAME}" reports Available as True with Reason Available
