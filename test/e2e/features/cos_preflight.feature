@BoxcutterRuntime
Feature: ClusterObjectSet preflight dry-run validation

  As an OLM user applying a ClusterObjectSet directly, I want the revision to be
  rejected with an actionable PreconditionsNotMet condition when one of its
  objects would be rejected by the API server, and to recover automatically once
  the underlying problem is resolved, so that transient or fixable issues do not
  require re-creating the revision.

  Background:
    Given OLM is available

  Scenario: ClusterObjectSet reports PreconditionsNotMet when an object targets a non-existent namespace
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        group: ${NAME}
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: cm-x
                namespace: ${COS_NAME}-absent
              data:
                k: v
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Ready as False with Reason PreconditionsNotMet and Message includes:
    """
      check rejected phase "resources": phase "resources": /v1, Kind=ConfigMap ${COS_NAME}-absent/cm-x: namespaces "${COS_NAME}-absent" not found.
    """

  Scenario: ClusterObjectSet reports PreconditionsNotMet when an object has an invalid spec
    Given namespace "${TEST_NAMESPACE}" is available
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        group: ${NAME}
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: Pod
              metadata:
                name: bad-pod
                namespace: ${TEST_NAMESPACE}
              spec:
                containers: []
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Ready as False with Reason PreconditionsNotMet and Message includes:
    """
      check rejected phase "resources": phase "resources": /v1, Kind=Pod ${TEST_NAMESPACE}/bad-pod: Pod "bad-pod" is invalid: spec.containers: Required value.
    """

  Scenario: ClusterObjectSet recovers once a missing target namespace is created
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        group: ${NAME}
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: cm-x
                namespace: ${COS_NAME}-recover
              data:
                k: v
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Ready as False with Reason PreconditionsNotMet and Message includes:
    """
      check rejected phase "resources": phase "resources": /v1, Kind=ConfigMap ${COS_NAME}-recover/cm-x: namespaces "${COS_NAME}-recover" not found.
    """
    When resource is applied
      """
      apiVersion: v1
      kind: Namespace
      metadata:
        name: ${COS_NAME}-recover
      """
    And ClusterObjectSet "${COS_NAME}" reconciliation is triggered
    Then ClusterObjectSet "${COS_NAME}" reports Ready as True with Reason AllObjectsReady
