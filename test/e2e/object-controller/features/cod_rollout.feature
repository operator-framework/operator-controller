@ObjectController @ClusterObjectDeployment
Feature: ClusterObjectDeployment revision rollout

  As an OLM user applying a ClusterObjectDeployment, I want the controller to roll
  out my template as an immutable ClusterObjectSet revision, create a new revision
  when the template changes, report rollout progress on the Progressing condition,
  and report availability on the Available condition.

  Background:
    Given object-controller is available

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
              - type: InlineManifest
                inlineManifest:
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
    And ClusterObjectDeployment "${NAME}" reports Available as True with Reason Available
    # The single revision is controller-owned by the deployment (the owns step verifies the controller UID).
    And ClusterObjectDeployment "${NAME}" owns 1 ClusterObjectSets

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
              - type: InlineManifest
                inlineManifest:
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
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
              - type: InlineManifest
                inlineManifest:
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
    And ClusterObjectDeployment "${NAME}" reports Available as True with Reason Available

  # Reapplying byte-identical content leaves the template hash unchanged, so the
  # controller must not cut a second revision.
  Scenario: Reapplying an unchanged template creates no new revision
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
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
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
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    Then ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectDeployment "${NAME}" consistently reports Progressing as False with Reason NewClusterObjectSetProgressed for "5s"
    And resource "clusterobjectset/${NAME}-2" is not found

  # Root-level metadata and spec.revisionHistoryLimit are outside the hashed
  # spec.template, so editing only those must not cut a new revision.
  Scenario: Editing only root metadata or revisionHistoryLimit creates no new revision
    Given namespace "${TEST_NAMESPACE}" is available
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
      spec:
        revisionHistoryLimit: 5
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
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    And ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
        labels:
          team: platform
      spec:
        revisionHistoryLimit: 3
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
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    Then ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectDeployment "${NAME}" consistently reports Progressing as False with Reason NewClusterObjectSetProgressed for "5s"
    And resource "clusterobjectset/${NAME}-2" is not found

  # Reverting content to a previous template (A then B then A) does not reuse the
  # original revision: revision numbers are max+1 across the group and never reused,
  # so the reverted content lands on a fresh revision 3.
  Scenario: Reverting template content cuts a fresh revision without reuse (A to B to A)
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
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: a
      """
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
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: b
      """
    And ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady
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
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: a
      """
    Then ClusterObjectSet "${NAME}-3" has group "${NAME}"
    And ClusterObjectSet "${NAME}-3" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "${NAME}" owns 3 ClusterObjectSets

  # A template update submitted while an earlier revision is still pending must
  # not strand the rollout: the controller cuts a fresh revision for the latest
  # template and converges to it, superseding the still-pending intermediate
  # revision. Revision 2 is held pending by colliding with a pre-existing,
  # unowned object (collisionProtection: Prevent); revision 3 drops the colliding
  # object and becomes the active revision.
  Scenario: A template update applied while a prior revision is pending converges to the latest
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
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v1
      """
    And ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    # Pre-create a conflicting, unowned object so the next revision stalls on a collision.
    When resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: cod-pending-block-cm
        namespace: ${TEST_NAMESPACE}
      data:
        k: pre-existing
      """
    # Revision 2 collides on the pre-existing object and stays pending.
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
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v2
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-pending-block-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: managed
      """
    Then ClusterObjectSet "${NAME}-2" reports Ready as False with Reason ObjectCollision
    # Before revision 2 completes, submit a third template that drops the colliding object.
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
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v3
      """
    # The rollout converges to the latest template: revision 3 becomes active, the
    # still-pending revision 2 is superseded (archived), and the deployment settles.
    Then ClusterObjectSet "${NAME}-3" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${NAME}-2" is archived
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectDeployment "${NAME}" owns 3 ClusterObjectSets
