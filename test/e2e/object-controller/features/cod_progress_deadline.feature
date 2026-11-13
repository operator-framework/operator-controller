@ObjectController @ClusterObjectDeployment @ProgressDeadline @CODDeadlineLifecycle
Feature: ClusterObjectDeployment progress deadline lifecycle

  A revision must recover within its deadline without creating another revision.
  After completion, health may regress while completion remains recorded.
  The deployment must keep reporting a successful rollout beyond the deadline.

  Background:
    Given object-controller is available
    And namespace "${TEST_NAMESPACE}" is available

  Scenario: Completed revision stays progressed when health regresses beyond the deadline
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
      spec:
        progressDeadlineMinutes: 1
        template:
          spec:
            collisionProtection: Prevent
            progressionProbes:
            - selector:
                type: Label
                label:
                  matchLabels:
                    probe: deadline-gate
              assertions:
              - type: FieldValue
                fieldValue:
                  fieldPath: data.ready
                  value: "true"
            phases:
            - name: resources
              objects:
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: deadline-gate
                    namespace: ${TEST_NAMESPACE}
                    labels:
                      probe: deadline-gate
                  data:
                    payload: unchanged
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as False with Reason ObjectsNotReady
    And resource "clusterobjectset/${NAME}-1" matches
      """
      status:
        completedAt: null
      """
    # Only the test owns data.ready, which is absent from the immutable manifest.
    When resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: deadline-gate
        namespace: ${TEST_NAMESPACE}
      data:
        ready: "true"
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectDeployment "${NAME}" reports Available as True with Reason Available
    And ClusterObjectDeployment "${NAME}" owns 1 ClusterObjectSets
    And ClusterObjectSet "${NAME}-1" has a completion timestamp
    When resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: deadline-gate
        namespace: ${TEST_NAMESPACE}
      data:
        ready: "false"
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as False with Reason ObjectsNotReady
    And ClusterObjectDeployment "${NAME}" reports Available as False with Reason Unavailable
    And ClusterObjectSet "${NAME}-1" has a completion timestamp
    # Both controllers have reflected the health regression. The successful
    # rollout reason must persist beyond the one-minute deadline.
    And ClusterObjectDeployment "${NAME}" consistently reports Progressing as False with Reason NewClusterObjectSetProgressed for "70s"
    When resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: deadline-gate
        namespace: ${TEST_NAMESPACE}
      data:
        ready: "true"
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "${NAME}" reports Available as True with Reason Available
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectSet "${NAME}-1" has a completion timestamp
    And ClusterObjectDeployment "${NAME}" owns 1 ClusterObjectSets
    And resource "configmap/deadline-gate" matches
      """
      data:
        payload: unchanged
        ready: "true"
      """

  Scenario: Revision recovers before its original progress deadline
    # Allow setup and reconciliation under parallel CI load.
    # The budget still starts when the revision is created.
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
      spec:
        progressDeadlineMinutes: 3
        template:
          spec:
            collisionProtection: Prevent
            progressionProbes:
            - selector:
                type: Label
                label:
                  matchLabels:
                    probe: deadline-gate
              assertions:
              - type: FieldValue
                fieldValue:
                  fieldPath: data.ready
                  value: "true"
            phases:
            - name: resources
              objects:
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: deadline-gate
                    namespace: ${TEST_NAMESPACE}
                    labels:
                      probe: deadline-gate
                  data:
                    payload: unchanged
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as False with Reason ObjectsNotReady
    And ClusterObjectDeployment "${NAME}" reports Progressing as True with Reason NewClusterObjectSetProgressing
    And ClusterObjectDeployment "${NAME}" reports Available as False with Reason Unavailable
    And ClusterObjectDeployment "${NAME}" owns 1 ClusterObjectSets
    And resource "clusterobjectset/${NAME}-1" matches
      """
      status:
        completedAt: null
      """
    When resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: deadline-gate
        namespace: ${TEST_NAMESPACE}
      data:
        ready: "true"
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectDeployment "${NAME}" reports Available as True with Reason Available
    And ClusterObjectDeployment "${NAME}" owns 1 ClusterObjectSets
    And resource "clusterobjectdeployment/${NAME}" matches
      """
      spec:
        progressDeadlineMinutes: 3
      """
    # Compare the named revision's completion against its server-assigned creation
    # timestamp plus the unchanged three-minute policy.
    And ClusterObjectSet "${NAME}-1" completed before the deadline of ClusterObjectDeployment "${NAME}"
    And resource "configmap/deadline-gate" matches
      """
      data:
        payload: unchanged
        ready: "true"
      """
