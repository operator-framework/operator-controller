@BoxcutterRuntime
Feature: ClusterObjectSet static preflight validation

  As an OLM user applying a ClusterObjectSet directly, I want the revision to be
  rejected with an actionable PreconditionsNotMet condition when its objects fail
  the preflight validation checks, so that malformed revisions surface clearly
  instead of being silently accepted.

  Background:
    Given OLM is available

  Scenario: ClusterObjectSet reports PreconditionsNotMet when the same object appears in multiple phases
    Given namespace "${TEST_NAMESPACE}" is available
    When ClusterObjectSet is applied
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
        - name: phase-a
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: dup-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: v
        - name: phase-b
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: dup-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: v
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Ready as False with Reason PreconditionsNotMet and Message includes:
    """
      A preflight check rejected the revision: revision "${COS_NAME}" (1): phase "phase-a": /v1, Kind=ConfigMap ${TEST_NAMESPACE}/dup-cm: duplicate object found in phases: phase-a, phase-b
    """

  Scenario: ClusterObjectSet reports PreconditionsNotMet when an object manifest carries ownerReferences
    Given namespace "${TEST_NAMESPACE}" is available
    When ClusterObjectSet is applied
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
        - name: phase-a
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: meta-cm
                namespace: ${TEST_NAMESPACE}
                ownerReferences:
                - apiVersion: v1
                  kind: ConfigMap
                  name: some-owner
                  uid: "11111111-1111-1111-1111-111111111111"
                  controller: true
              data:
                k: v
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Ready as False with Reason PreconditionsNotMet and Message includes:
    """
      A preflight check rejected the revision: revision "${COS_NAME}" (1): phase "phase-a": /v1, Kind=ConfigMap ${TEST_NAMESPACE}/meta-cm: metadata.ownerReferences: Forbidden: must be empty.
    """
