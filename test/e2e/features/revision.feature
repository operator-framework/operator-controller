@BoxcutterRuntime
Feature: Install ClusterExtensionRevision

  As an OLM user I would like to install a cluster extension revision directly, without using the cluster extension API.

  Background:
    Given OLM is available

  Scenario: Probe failure for PersistentVolumeClaim halts phase progression
    Given ServiceAccount "pvc-probe-sa" with needed permissions is available in test namespace
    When ClusterExtensionRevision is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtensionRevision
      metadata:
        annotations:
          olm.operatorframework.io/service-account-name: pvc-probe-sa
          olm.operatorframework.io/service-account-namespace: ${TEST_NAMESPACE}
        name: ${CER_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        phases:
        - name: pvc
          objects:
          - object:
              apiVersion: v1
              kind: PersistentVolumeClaim
              metadata:
                name: test-pvc
                namespace: ${TEST_NAMESPACE}
              spec:
                accessModes:
                - ReadWriteOnce
                storageClassName: ""
                volumeName: test-pv
                resources:
                  requests:
                    storage: 1Mi
        - name: configmap
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: test-configmap
                namespace: ${TEST_NAMESPACE}
              data:
                name: test-configmap
                version: v1.2.0
        revision: 1
      """

    Then resource "persistentvolumeclaim/test-pvc" is installed
    And ClusterExtensionRevision "${CER_NAME}" reports Available as False with Reason ProbeFailure and Message:
    """
      Object PersistentVolumeClaim.v1 ${TEST_NAMESPACE}/test-pvc: persistentvolumeclaim phase must be "Bound"
    """
    And resource "configmap/test-configmap" is not installed

  Scenario: Phases progress when PersistentVolumeClaim becomes "Bound"
    Given ServiceAccount "pvc-probe-sa" with needed permissions is available in test namespace
    When ClusterExtensionRevision is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtensionRevision
      metadata:
        annotations:
          olm.operatorframework.io/service-account-name: pvc-probe-sa
          olm.operatorframework.io/service-account-namespace: ${TEST_NAMESPACE}
        name: ${CER_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        phases:
        - name: pvc
          objects:
          - object:
              apiVersion: v1
              kind: PersistentVolumeClaim
              metadata:
                name: test-pvc
                namespace: ${TEST_NAMESPACE}
              spec:
                accessModes:
                - ReadWriteOnce
                storageClassName: ""
                volumeName: test-pv
                resources:
                  requests:
                    storage: 1Mi
          - object:
              apiVersion: v1
              kind: PersistentVolume
              metadata:
                name: test-pv
              spec:
                accessModes:
                - ReadWriteOnce
                capacity:
                  storage: 1Mi
                claimRef:
                  apiVersion: v1
                  kind: PersistentVolumeClaim
                  name: test-pvc
                  namespace: ${TEST_NAMESPACE}
                persistentVolumeReclaimPolicy: Delete
                storageClassName: ""
                volumeMode: Filesystem
                local:
                  path: /tmp/persistent-volume
                nodeAffinity:
                  required:
                    nodeSelectorTerms:
                    - matchExpressions:
                      - key: kubernetes.io/hostname
                        operator: NotIn
                        values:
                        - a-node-name-that-should-not-exist
        - name: configmap
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: test-configmap
                namespace: ${TEST_NAMESPACE}
              data:
                name: test-configmap
                version: v1.2.0
        revision: 1
      """

    Then ClusterExtensionRevision "${CER_NAME}" reports Progressing as True with Reason Succeeded
    And ClusterExtensionRevision "${CER_NAME}" reports Available as True with Reason ProbesSucceeded
    And resource "persistentvolume/test-pv" is installed
    And resource "persistentvolumeclaim/test-pvc" is installed
    And resource "configmap/test-configmap" is installed
