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
      Object PersistentVolumeClaim.v1 ${TEST_NAMESPACE}/test-pvc: value at key "status.phase" != "Bound"; expected: "Bound" got: "Pending"
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

  Scenario: Phases does not progress when user-provided progressionProbes do not pass
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
        progressionProbes:
        - selector:
            type: Label
            label:
              matchLabels:
                test-label: foo
          assertions:
          - type: FieldValue
            fieldValue:
              fieldPath: data.foo
              value: bar
        phases:
        - name: cm-1
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: test-configmap-1
                namespace: ${TEST_NAMESPACE}
                labels:
                  test-label: foo
              data:
                foo: foo
        - name: cm-2
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: test-configmap-2
                namespace: ${TEST_NAMESPACE}
                labels:
                  test-label: bar
              data:
                name: test-configmap
                version: v1.2.0
        revision: 1
      """

    Then resource "configmap/test-configmap-1" is installed
    And ClusterExtensionRevision "${CER_NAME}" reports Available as False with Reason ProbeFailure and Message:
    """
      Object ConfigMap.v1 ${TEST_NAMESPACE}/test-configmap-1: value at key "data.foo" != "bar"; expected: "bar" got: "foo"
    """
    And resource "configmap/test-configmap-2" is not installed

  Scenario: Phases progresses when user-provided progressionProbes pass
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
        progressionProbes:
        - selector:
            type: GroupKind
            groupKind:
              group: ""
              kind: ConfigMap
          assertions:
          - type: FieldValue
            fieldValue:
              fieldPath: data.foo
              value: bar
        - selector:
            type: GroupKind
            groupKind:
              group: ""
              kind: ServiceAccount
          assertions:
          - type: FieldsEqual
            fieldsEqual:
              fieldA: "metadata.labels.foo"
              fieldB: "metadata.labels.bar"
        - selector:
            type: Label
            label:
              matchExpressions:
                - { key: expkey, operator: In, values: [exercise-label-selector-matchexpressions] }
          assertions:
          - type: ConditionEqual
            conditionEqual:
              type: "Ready"
              status: "True"
        phases:
        - name: phase1
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: test-configmap-1
                namespace: ${TEST_NAMESPACE}
                labels:
                  test-label: foo
              data:
                foo: bar
        - name: phase2
          objects:
          - object: 
              apiVersion: v1
              kind: ServiceAccount
              metadata:
                name: test-serviceaccount
                namespace: ${TEST_NAMESPACE}
                labels:
                  foo: exercise-fieldsEqual-probe
                  bar: exercise-fieldsEqual-probe
        - name: phase3
          objects:
          - object:
              apiVersion: v1
              kind: Pod
              metadata:
                name: test-pod
                namespace: ${TEST_NAMESPACE}
                labels:
                  expkey: exercise-label-selector-matchexpressions
              spec:
                containers:
                - command:
                  - "sleep"
                  args:
                  - "1000"
                  image: busybox:1.36
                  imagePullPolicy: IfNotPresent
                  name: busybox
                  securityContext:
                    runAsNonRoot: true
                    runAsUser: 1000
                    allowPrivilegeEscalation: false
                    capabilities:
                      drop:
                      - ALL
                    seccompProfile:
                      type: RuntimeDefault
        - name: phase4
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: test-configmap-3
                namespace: ${TEST_NAMESPACE}
                labels:
                  test-label: bar
              data:
                if-this-configmap-is-installed: all-prior-phase-probes-have-succeeded
                foo: bar
        revision: 1
      """

    Then resource "configmap/test-configmap-1" is installed
    And resource "serviceaccount/test-serviceaccount" is installed
    And resource "pod/test-pod" is installed
    And resource "configmap/test-configmap-3" is installed
    And ClusterExtensionRevision "${CER_NAME}" reports Progressing as True with Reason Succeeded
    And ClusterExtensionRevision "${CER_NAME}" reports Available as True with Reason ProbesSucceeded