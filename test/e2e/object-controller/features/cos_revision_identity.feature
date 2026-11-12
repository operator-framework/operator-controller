@ObjectController @ClusterObjectSet
Feature: Validate ClusterObjectSet revision identity

  A ClusterObjectSet's name must be <group>-<revision>, with the revision in
  decimal and without leading zeros. This lets Kubernetes name uniqueness
  prevent duplicate revision numbers within a group, regardless of ownership.

  Background:
    Given object-controller is available

  Scenario Outline: Reject a ClusterObjectSet name with <mismatch>
    When resource apply fails with error msg containing "metadata.name must be <spec.group>-<spec.revision>"
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: <name>
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
                name: cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: v
      """
    Then resource "clusterobjectset/<name>" is eventually not found

    Examples:
      | mismatch                      | name            |
      | a different group             | ${NAME}-other-1 |
      | a different revision          | ${NAME}-2       |
      | a revision with leading zeros | ${NAME}-01      |

  Scenario: A group cannot contain duplicate revision numbers
    Given ClusterObjectSet is applied
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
                name: cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: v
      """
    And resource "clusterobjectset/${NAME}-1" UID is remembered
    # The existing COS is Active. Request Archived in the duplicate so that an
    # accidental update would change its state. After the create is rejected,
    # assert that the existing COS is still Active.
    When resource create fails with error msg containing "already exists"
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${NAME}-1
      spec:
        group: ${NAME}
        revision: 1
        lifecycleState: Archived
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - type: InlineManifest
            inlineManifest:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: v
      """
    Then resource "clusterobjectset/${NAME}-1" keeps its UID
    And resource "clusterobjectset/${NAME}-1" matches
      """
      spec:
        group: ${NAME}
        revision: 1
        lifecycleState: Active
        phases:
        - name: resources
          objects:
          - type: InlineManifest
            inlineManifest:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: v
      """
