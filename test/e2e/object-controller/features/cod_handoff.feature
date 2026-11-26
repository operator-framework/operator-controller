@ObjectController @ClusterObjectDeployment
Feature: ClusterObjectDeployment multi-revision handoff

  As an OLM user applying a ClusterObjectDeployment, I want a blocked successor
  revision to leave the previous revision in place until it can take over, a
  managed object shared across revisions to survive the handoff without being
  recreated, and objects dropped from the template to be garbage collected, so
  that rollouts are safe and leave no orphaned content behind.

  Background:
    Given object-controller is available

  # While the successor revision cannot take ownership of its objects it never
  # becomes Ready, so both revisions remain and the deployment reports the hard
  # stall. Removing the blocker lets the successor complete and the predecessor
  # is archived; the object shared by both revisions keeps its identity (UID)
  # across the handoff rather than being deleted and recreated.
  Scenario: A blocked successor keeps the predecessor until it is unblocked
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
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-shared-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v1
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And resource "configmap/cod-shared-cm" UID is remembered
    # Pre-create a conflicting, unowned object the successor revision will collide on.
    When resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: cod-block-cm
        namespace: ${TEST_NAMESPACE}
      data:
        k: pre-existing
      """
    # Update the template: bump the shared object and add the colliding object.
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
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-shared-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v2
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-block-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: managed
      """
    Then ClusterObjectSet "${NAME}-2" reports Ready as False with Reason ObjectCollision
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason RevisionObjectCollision
    And ClusterObjectDeployment "${NAME}" owns 2 ClusterObjectSets
    # The predecessor must stay active while the successor is blocked. owns-2
    # only checks group membership and would also count an archived revision.
    # Readiness is not a reliable signal here: the successor adopts the shared
    # object before hitting the collision, so the predecessor reports
    # Ready=False/Superseded while its lifecycle is still Active. Assert the
    # lifecycle directly to confirm it is not prematurely archived.
    And resource "clusterobjectset/${NAME}-1" matches
      """
      spec:
        lifecycleState: Active
      """
    # Unblock: remove the conflicting object so the successor can adopt its objects.
    When resource "configmap/cod-block-cm" is removed
    Then ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${NAME}-1" is archived
    And resource "configmap/cod-shared-cm" keeps its UID

  # An object present in an earlier revision but absent from the successor's
  # template is cleaned up once the successor completes.
  Scenario: An object dropped across a revision is garbage collected
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
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-keep-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-drop-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
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
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-keep-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    Then ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady
    And resource "configmap/cod-drop-cm" is eventually not found

  Scenario: Completion does not archive a foreign-owned same-group predecessor
    Given namespace "${TEST_NAMESPACE}" is available
    When ClusterObjectSet with Namespace owner "${TEST_NAMESPACE}" is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${NAME}-1
      spec:
        group: ${NAME}
        revision: 1
        lifecycleState: Active
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - type: InlineManifest
            inlineManifest:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: cod-foreign-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: foreign
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    # Create an adoptable head so the foreign predecessor does not
    # block COD revision creation as the highest active group member.
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${NAME}-2
        labels:
          app.kubernetes.io/managed-by: object-controller
      spec:
        group: ${NAME}
        revision: 2
        lifecycleState: Active
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - type: InlineManifest
            inlineManifest:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: cod-intermediate-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: intermediate
      """
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
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-owned-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: owned
      """
    Then ClusterObjectSet "${NAME}-3" reports Ready as True with Reason AllObjectsReady
    # COD must have observed completion, not merely created the successor.
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectSet "${NAME}-2" is archived
    And resource "clusterobjectset/${NAME}-1" matches
      """
      spec:
        lifecycleState: Active
      """
    And resource "configmap/cod-foreign-cm" is installed
