@BoxcutterRuntime
Feature: Track ClusterObjectSet revisions by group

  As a ClusterObjectSet user, I want to group successive revisions so that a new
  revision can update the Kubernetes objects managed by an earlier revision
  without deleting and recreating them. This handoff should work even when the
  ClusterObjectSets have different controller owners or no owner. I also want
  collision protection between groups, and pruning a group's old revisions
  should leave other groups' revisions untouched.

  Background:
    Given OLM is available
    And an image registry is available
    And namespace "${TEST_NAMESPACE}" is available

  Scenario Outline: Group handoff preserves objects when <revision-ownership>
    Both ClusterObjectSets have the same group. Their controller owner references
    point to different Namespaces, or one or both ClusterObjectSets have no owner
    reference. These owner references must not determine group membership.

    The new revision must update the existing ConfigMap without deleting and
    recreating it. The UID saved before handoff must still match after the new
    revision is ready and the previous revision is archived.

    Given resource is applied
      """
      apiVersion: v1
      kind: Namespace
      metadata:
        name: ${TEST_NAMESPACE}-other
      """
    And ClusterObjectSet with Namespace owner "<lower-revision-owner>" is applied
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
        - name: config
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: ${NAME}-payload
                namespace: ${TEST_NAMESPACE}
              data:
                revision: "1"
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${NAME}-1" phase objects use SSA manager "cos-group/${NAME}"
    And resource "configmap/${NAME}-payload" UID is remembered
    When ClusterObjectSet with Namespace owner "<higher-revision-owner>" is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${NAME}-2
      spec:
        group: ${NAME}
        revision: 2
        lifecycleState: Active
        collisionProtection: Prevent
        phases:
        - name: config
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: ${NAME}-payload
                namespace: ${TEST_NAMESPACE}
              data:
                revision: "2"
      """
    Then ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${NAME}-1" is archived
    And ClusterObjectSet "${NAME}-2" phase objects use SSA manager "cos-group/${NAME}"
    And resource "configmap/${NAME}-payload" matches
      """
      data:
        revision: "2"
      """
    And resource "configmap/${NAME}-payload" keeps its UID

    # An empty owner creates an ownerless COS. Namespace owners are real,
    # cluster-scoped objects, independent of CE and future COD controllers.
    Examples:
      | revision-ownership                                     | lower-revision-owner | higher-revision-owner   |
      | revisions have different controller owners             | ${TEST_NAMESPACE}    | ${TEST_NAMESPACE}-other |
      | an owned revision is followed by an ownerless revision | ${TEST_NAMESPACE}    |                         |
      | an ownerless revision is followed by an owned revision |                      | ${TEST_NAMESPACE}       |
      | both revisions have no controller owner                |                      |                         |

  Scenario: Different groups collide even with the same controller owner
    Given ClusterObjectSet with Namespace owner "${TEST_NAMESPACE}" is applied
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
        - name: config
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: ${NAME}-payload
                namespace: ${TEST_NAMESPACE}
              data:
                revision: "1"
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And resource "configmap/${NAME}-payload" UID is remembered
    When ClusterObjectSet with Namespace owner "${TEST_NAMESPACE}" is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${NAME}-other-2
      spec:
        group: ${NAME}-other
        revision: 2
        lifecycleState: Active
        collisionProtection: Prevent
        phases:
        - name: config
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: ${NAME}-payload
                namespace: ${TEST_NAMESPACE}
              data:
                revision: "2"
      """
    Then ClusterObjectSet "${NAME}-other-2" reports Ready as False with Reason ObjectCollision and Message includes:
      """
      Cannot take ownership of 1 object(s)
      """
    And ClusterObjectSet "${NAME}-1" phase objects use SSA manager "cos-group/${NAME}"
    And resource "configmap/${NAME}-payload" matches
      """
      data:
        revision: "1"
      """
    And resource "configmap/${NAME}-payload" keeps its UID
    And resource "clusterobjectset/${NAME}-1" matches
      """
      spec:
        lifecycleState: Active
      """

  Scenario: Revision pruning is isolated by group
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.0.0   | beta    |          | CRD, Deployment, ConfigMap |
      | test    | 1.0.1   | beta    | 1.0.0    | CRD, Deployment, ConfigMap |
      | test    | 1.0.2   | beta    | 1.0.1    | CRD, Deployment, ConfigMap |
      | test    | 1.0.3   | beta    | 1.0.2    | CRD, Deployment, ConfigMap |
      | test    | 1.0.4   | beta    | 1.0.3    | CRD, Deployment, ConfigMap |
      | test    | 1.0.5   | beta    | 1.0.4    | CRD, Deployment, ConfigMap |
      | test    | 1.0.6   | beta    | 1.0.5    | CRD, Deployment, ConfigMap |
    And ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
        labels:
          olm.operatorframework.io/owner-name: ${NAME}
      spec:
        group: ${NAME}-other
        revision: 1
        lifecycleState: Archived
        collisionProtection: Prevent
      """
    And ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            version: 1.0.0
            selector:
              matchLabels:
                olm.operatorframework.io/metadata.name: ${CATALOG:test}
      """
    And ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.1"
    Then ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.2"
    Then ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.3"
    Then ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.4"
    Then ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.5"
    Then ClusterExtension is rolled out
    When ClusterExtension version is updated to "1.0.6"
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterObjectSet "${NAME}-7" has group "${NAME}"
    And resource "clusterobjectset/${NAME}-1" is eventually not found
    # The existing retention policy keeps five prior revisions plus the current one.
    And ClusterExtension "${NAME}" owns 6 ClusterObjectSets
    And resource "clusterobjectset/${COS_NAME}" exists
    And ClusterObjectSet "${COS_NAME}" has group "${NAME}-other"
