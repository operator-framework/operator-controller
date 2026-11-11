@ObjectController @ClusterObjectDeployment
Feature: ClusterObjectDeployment group isolation and deletion

  As an OLM user running several ClusterObjectDeployments, I want each deployment
  to own only the revisions in its own group and to tear down its revisions and
  managed objects when deleted, so that deployments do not interfere with one
  another and leave nothing behind.

  Background:
    Given object-controller is available

  # Two deployments allocate distinct groups and must not adopt each other's
  # revisions: each owns exactly its own revision (the owns step verifies the
  # controller UID on every revision in the group).
  Scenario: Two ClusterObjectDeployments do not adopt each other's revisions
    Given namespace "${TEST_NAMESPACE}" is available
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}-a
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
                    name: cod-a-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    And resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}-b
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
                    name: cod-b-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    Then ClusterObjectSet "${NAME}-a-1" has group "${NAME}-a"
    And ClusterObjectSet "${NAME}-a-1" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${NAME}-b-1" has group "${NAME}-b"
    And ClusterObjectSet "${NAME}-b-1" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "${NAME}-a" owns 1 ClusterObjectSets
    And ClusterObjectDeployment "${NAME}-b" owns 1 ClusterObjectSets

  # Deleting the deployment cascades through owner references: its revision and the
  # objects that revision manages are removed.
  Scenario: Deleting a ClusterObjectDeployment tears down its revisions and managed objects
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
                    name: cod-teardown-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    When resource "clusterobjectdeployment/${NAME}" is removed
    Then ClusterObjectSet "${NAME}-1" is eventually deleted
    And resource "configmap/cod-teardown-cm" is eventually not found
