@ObjectController @ClusterObjectSet @COSObjectLifecycle
Feature: Standalone ClusterObjectSet object lifecycle

  Background:
    Given object-controller is available
    And namespace "${TEST_NAMESPACE}" is available

  Scenario: Objects omitted from a successor are collected while retained objects are preserved
    # COS phases are immutable. Apply a new COS revision directly, without a COD.
    When ClusterObjectSet is applied
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
          - type: InlineManifest
            inlineManifest:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: retained
                namespace: ${TEST_NAMESPACE}
              data:
                value: unchanged
          - type: InlineManifest
            inlineManifest:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: removed
                namespace: ${TEST_NAMESPACE}
              data:
                value: obsolete
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And resource "configmap/removed" is installed
    And resource "configmap/retained" UID is remembered
    When ClusterObjectSet is applied
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
          - type: InlineManifest
            inlineManifest:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: retained
                namespace: ${TEST_NAMESPACE}
              data:
                value: unchanged
      """
    Then ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${NAME}-1" is archived
    And resource "configmap/removed" is eventually not found
    And resource "configmap/retained" keeps its UID
    And resource "configmap/retained" matches
      """
      data:
        value: unchanged
      """
    And ClusterObjectSet "${NAME}-2" reports aggregate object counts: total=1, present=1, synced=1, ready=1

  Scenario: Later phases stay blocked across reconciles until each earlier probe passes
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        name: ${COS_NAME}
      spec:
        group: ${NAME}
        revision: 1
        lifecycleState: Active
        collisionProtection: Prevent
        progressionProbes:
        - selector:
            type: Label
            label:
              matchLabels:
                probe: gated
          assertions:
          - type: FieldValue
            fieldValue:
              fieldPath: metadata.annotations.ready
              value: "true"
        phases:
        - name: first
          objects:
          - type: InlineManifest
            inlineManifest:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: first
                namespace: ${TEST_NAMESPACE}
                labels:
                  probe: gated
              data:
                phase: first
        - name: second
          objects:
          - type: InlineManifest
            inlineManifest:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: second
                namespace: ${TEST_NAMESPACE}
                labels:
                  probe: gated
              data:
                phase: second
        - name: third
          objects:
          - type: InlineManifest
            inlineManifest:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: third
                namespace: ${TEST_NAMESPACE}
              data:
                phase: third
      """
    Then resource "configmap/first" is installed
    And ClusterObjectSet "${COS_NAME}" reports Ready as False with Reason ObjectsNotReady
    And ClusterObjectSet "${COS_NAME}" keeps later phase resources absent for "5s":
      | resource         |
      | configmap/second |
      | configmap/third  |
    # Restoration proves the controller reconciled again with the probe blocked.
    When resource "configmap/first" is removed
    Then resource "configmap/first" is eventually restored
    And ClusterObjectSet "${COS_NAME}" keeps later phase resources absent for "5s":
      | resource         |
      | configmap/second |
      | configmap/third  |
    # The readiness annotation is external to the immutable COS manifest.
    When annotations are added to "configmap/first"
      | key   | value |
      | ready | true  |
    Then resource "configmap/second" is installed
    And ClusterObjectSet "${COS_NAME}" keeps later phase resources absent for "5s":
      | resource        |
      | configmap/third |
    When resource "configmap/second" is removed
    Then resource "configmap/second" is eventually restored
    And ClusterObjectSet "${COS_NAME}" keeps later phase resources absent for "5s":
      | resource        |
      | configmap/third |
    When annotations are added to "configmap/second"
      | key   | value |
      | ready | true  |
    Then ClusterObjectSet "${COS_NAME}" reports Ready as True with Reason AllObjectsReady
    And resource "configmap/third" is installed
    And ClusterObjectSet "${COS_NAME}" reports aggregate object counts: total=3, present=3, synced=3, ready=3
