@BoxcutterRuntime
Feature: ClusterObjectSet object collision and adoption

  As an OLM user applying a ClusterObjectSet directly, I want the revision's
  collisionProtection setting to control whether it refuses to manage objects
  that already exist (reporting ObjectCollision) or adopts them, so that I can
  choose how aggressively a revision takes ownership of pre-existing objects.

  Background:
    Given OLM is available

  # ---------------------------------------------------------------------------
  # Collisions: the revision refuses to take ownership (Ready=False/ObjectCollision)
  # ---------------------------------------------------------------------------

  Scenario: Prevent reports ObjectCollision for a pre-existing unowned object
    Given namespace "${TEST_NAMESPACE}" is available
    And resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: shared-cm
        namespace: ${TEST_NAMESPACE}
      data:
        k: pre-existing
      """
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        group: ${NAME}
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: shared-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: managed
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Ready as False with Reason ObjectCollision and Message includes:
    """
      Cannot take ownership of 1 object(s) in phase "resources" because they are owned by another controller or already exist.
    """
    And resource "configmap/shared-cm" matches
      """
      data:
        k: pre-existing
      """

  Scenario: Prevent reports ObjectCollision for an object owned by another revision
    Given namespace "${TEST_NAMESPACE}" is available
    And ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: owner-${NAME}-1
      spec:
        lifecycleState: Active
        group: owner-${NAME}
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: shared-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: owned-by-owner
        revision: 1
      """
    And ClusterObjectSet "owner-${NAME}-1" reports Ready as True with Reason AllObjectsReady
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: main-${NAME}-1
      spec:
        lifecycleState: Active
        group: main-${NAME}
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: shared-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: contested
        revision: 1
      """
    Then ClusterObjectSet "main-${NAME}-1" reports Ready as False with Reason ObjectCollision and Message includes:
    """
      Cannot take ownership of 1 object(s) in phase "resources" because they are owned by another controller or already exist.
    """

  Scenario: IfNoController reports ObjectCollision for an object owned by another revision
    Given namespace "${TEST_NAMESPACE}" is available
    And ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: owner-${NAME}-1
      spec:
        lifecycleState: Active
        group: owner-${NAME}
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: shared-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: owned-by-owner
        revision: 1
      """
    And ClusterObjectSet "owner-${NAME}-1" reports Ready as True with Reason AllObjectsReady
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: main-${NAME}-1
      spec:
        lifecycleState: Active
        group: main-${NAME}
        collisionProtection: IfNoController
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: shared-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: contested
        revision: 1
      """
    Then ClusterObjectSet "main-${NAME}-1" reports Ready as False with Reason ObjectCollision and Message includes:
    """
      Cannot take ownership of 1 object(s) in phase "resources" because they are owned by another controller or already exist.
    """

  # ---------------------------------------------------------------------------
  # Adoption: the revision takes ownership (Ready=True/AllObjectsReady)
  # ---------------------------------------------------------------------------

  Scenario: IfNoController adopts a pre-existing unowned object
    Given namespace "${TEST_NAMESPACE}" is available
    And resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: shared-cm
        namespace: ${TEST_NAMESPACE}
      data:
        k: pre-existing
      """
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        group: ${NAME}
        collisionProtection: IfNoController
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: shared-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: managed
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Ready as True with Reason AllObjectsReady
    And resource "configmap/shared-cm" matches
      """
      data:
        k: managed
      """

  Scenario: None adopts a pre-existing unowned object
    Given namespace "${TEST_NAMESPACE}" is available
    And resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: shared-cm
        namespace: ${TEST_NAMESPACE}
      data:
        k: pre-existing
      """
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        group: ${NAME}
        collisionProtection: None
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: shared-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: managed
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Ready as True with Reason AllObjectsReady
    And resource "configmap/shared-cm" matches
      """
      data:
        k: managed
      """

  Scenario: None adopts an object owned by another revision
    Given namespace "${TEST_NAMESPACE}" is available
    And ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: owner-${NAME}-1
      spec:
        lifecycleState: Active
        group: owner-${NAME}
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: shared-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: owned-by-owner
        revision: 1
      """
    And ClusterObjectSet "owner-${NAME}-1" reports Ready as True with Reason AllObjectsReady
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: main-${NAME}-1
      spec:
        lifecycleState: Active
        group: main-${NAME}
        collisionProtection: None
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: shared-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: contested
        revision: 1
      """
    Then ClusterObjectSet "main-${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And resource "configmap/shared-cm" matches
      """
      data:
        k: contested
      """

  # ---------------------------------------------------------------------------
  # Recovery: a collision clears once the conflicting object is removed
  # ---------------------------------------------------------------------------

  Scenario: ClusterObjectSet recovers from ObjectCollision once the conflicting object is removed
    Given namespace "${TEST_NAMESPACE}" is available
    And resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: shared-cm
        namespace: ${TEST_NAMESPACE}
      data:
        k: pre-existing
      """
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        group: ${NAME}
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: shared-cm
                namespace: ${TEST_NAMESPACE}
              data:
                k: managed
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Ready as False with Reason ObjectCollision and Message includes:
    """
      Cannot take ownership of 1 object(s) in phase "resources" because they are owned by another controller or already exist.
    """
    When resource "configmap/shared-cm" is removed
    And ClusterObjectSet "${COS_NAME}" reconciliation is triggered
    Then ClusterObjectSet "${COS_NAME}" reports Ready as True with Reason AllObjectsReady
    And resource "configmap/shared-cm" matches
      """
      data:
        k: managed
      """
