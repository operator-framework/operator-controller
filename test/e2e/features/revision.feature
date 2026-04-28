@BoxcutterRuntime
Feature: Install ClusterObjectSet

  As an OLM user I would like to install a ClusterObjectSet directly, without using the cluster extension API.

  Background:
    Given OLM is available

  Scenario: Probe failure for PersistentVolumeClaim halts phase progression
    Given ServiceAccount "pvc-probe-sa" with needed permissions is available in test namespace
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        annotations:
          olm.operatorframework.io/service-account-name: pvc-probe-sa
          olm.operatorframework.io/service-account-namespace: ${TEST_NAMESPACE}
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        progressionProbes:
        - selector:
            type: GroupKind
            groupKind:
              group: ""
              kind: PersistentVolumeClaim
          assertions:
          - type: FieldValue
            fieldValue:
              fieldPath: "status.phase"
              value: "Bound"
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
    And ClusterObjectSet "${COS_NAME}" reports Available as False with Reason ProbeFailure and Message:
    """
      Object PersistentVolumeClaim.v1 ${TEST_NAMESPACE}/test-pvc: value at key "status.phase" != "Bound"; expected: "Bound" got: "Pending"
    """
    And resource "configmap/test-configmap" is not installed

  Scenario: Phases progress when PersistentVolumeClaim becomes "Bound"
    Given ServiceAccount "pvc-probe-sa" with needed permissions is available in test namespace
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        annotations:
          olm.operatorframework.io/service-account-name: pvc-probe-sa
          olm.operatorframework.io/service-account-namespace: ${TEST_NAMESPACE}
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        progressionProbes:
        - selector:
            type: GroupKind
            groupKind:
              group: ""
              kind: PersistentVolumeClaim
          assertions:
          - type: FieldValue
            fieldValue:
              fieldPath: "status.phase"
              value: "Bound"
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

    Then ClusterObjectSet "${COS_NAME}" reports Progressing as True with Reason Succeeded
    And ClusterObjectSet "${COS_NAME}" reports Available as True with Reason ProbesSucceeded
    And resource "persistentvolume/test-pv" is installed
    And resource "persistentvolumeclaim/test-pvc" is installed
    And resource "configmap/test-configmap" is installed

  Scenario: Phases does not progress when user-provided progressionProbes do not pass
    Given ServiceAccount "pvc-probe-sa" with needed permissions is available in test namespace
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        annotations:
          olm.operatorframework.io/service-account-name: pvc-probe-sa
          olm.operatorframework.io/service-account-namespace: ${TEST_NAMESPACE}
        name: ${COS_NAME}
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
    And ClusterObjectSet "${COS_NAME}" reports Available as False with Reason ProbeFailure and Message:
    """
      Object ConfigMap.v1 ${TEST_NAMESPACE}/test-configmap-1: value at key "data.foo" != "bar"; expected: "bar" got: "foo"
    """
    And resource "configmap/test-configmap-2" is not installed

  Scenario: Phases progresses when user-provided progressionProbes pass
    Given ServiceAccount "pvc-probe-sa" with needed permissions is available in test namespace
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        annotations:
          olm.operatorframework.io/service-account-name: pvc-probe-sa
          olm.operatorframework.io/service-account-namespace: ${TEST_NAMESPACE}
        name: ${COS_NAME}
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
    And ClusterObjectSet "${COS_NAME}" reports Progressing as True with Reason Succeeded
    And ClusterObjectSet "${COS_NAME}" reports Available as True with Reason ProbesSucceeded

  Scenario: User can install a ClusterObjectSet with objects stored in Secrets
    Given ServiceAccount "olm-sa" with needed permissions is available in test namespace
    When resource is applied
      """
      apiVersion: v1
      kind: Secret
      metadata:
        name: ${COS_NAME}-ref-secret
        namespace: ${TEST_NAMESPACE}
      immutable: true
      type: olm.operatorframework.io/object-data
      stringData:
        configmap: |
          {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
              "name": "test-configmap-ref",
              "namespace": "${TEST_NAMESPACE}"
            },
            "data": {
              "key": "value"
            }
          }
        deployment: |
          {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {
              "name": "test-httpd",
              "namespace": "${TEST_NAMESPACE}",
              "labels": {
                "app": "test-httpd"
              }
            },
            "spec": {
              "replicas": 1,
              "selector": {
                "matchLabels": {
                  "app": "test-httpd"
                }
              },
              "template": {
                "metadata": {
                  "labels": {
                    "app": "test-httpd"
                  }
                },
                "spec": {
                  "containers": [
                    {
                      "name": "httpd",
                      "image": "busybox:1.36",
                      "imagePullPolicy": "IfNotPresent",
                      "command": ["httpd"],
                      "args": ["-f", "-p", "8080"],
                      "securityContext": {
                        "runAsNonRoot": true,
                        "runAsUser": 1000,
                        "allowPrivilegeEscalation": false,
                        "capabilities": {
                          "drop": ["ALL"]
                        },
                        "seccompProfile": {
                          "type": "RuntimeDefault"
                        }
                      }
                    }
                  ]
                }
              }
            }
          }
      """
    And ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        annotations:
          olm.operatorframework.io/service-account-name: olm-sa
          olm.operatorframework.io/service-account-namespace: ${TEST_NAMESPACE}
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - ref:
              name: ${COS_NAME}-ref-secret
              namespace: ${TEST_NAMESPACE}
              key: configmap
          - ref:
              name: ${COS_NAME}-ref-secret
              namespace: ${TEST_NAMESPACE}
              key: deployment
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Progressing as True with Reason Succeeded
    And ClusterObjectSet "${COS_NAME}" reports Available as True with Reason ProbesSucceeded
    And resource "configmap/test-configmap-ref" is installed
    And resource "deployment/test-httpd" is installed
    And ClusterObjectSet "${COS_NAME}" has observed phase "resources" with a non-empty digest

  Scenario: ClusterObjectSet blocks reconciliation when referenced Secret is mutable
    Given ServiceAccount "olm-sa" with needed permissions is available in test namespace
    And resource is applied
      """
      apiVersion: v1
      kind: Secret
      metadata:
        name: ${COS_NAME}-mutable-secret
        namespace: ${TEST_NAMESPACE}
      type: olm.operatorframework.io/object-data
      stringData:
        configmap: |
          {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
              "name": "test-cm-mutable",
              "namespace": "${TEST_NAMESPACE}"
            },
            "data": {
              "key": "value"
            }
          }
      """
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        annotations:
          olm.operatorframework.io/service-account-name: olm-sa
          olm.operatorframework.io/service-account-namespace: ${TEST_NAMESPACE}
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - ref:
              name: ${COS_NAME}-mutable-secret
              namespace: ${TEST_NAMESPACE}
              key: configmap
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Progressing as False with Reason Blocked and Message:
    """
      the following secrets are not immutable (referenced secrets must have immutable set to true): ${TEST_NAMESPACE}/${COS_NAME}-mutable-secret
    """

  Scenario: ClusterObjectSet blocks reconciliation when referenced Secret content changes
    Given ServiceAccount "olm-sa" with needed permissions is available in test namespace
    When resource is applied
      """
      apiVersion: v1
      kind: Secret
      metadata:
        name: ${COS_NAME}-change-secret
        namespace: ${TEST_NAMESPACE}
      immutable: true
      type: olm.operatorframework.io/object-data
      stringData:
        configmap: |
          {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
              "name": "test-cm-change",
              "namespace": "${TEST_NAMESPACE}"
            },
            "data": {
              "key": "original-value"
            }
          }
      """
    And ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        annotations:
          olm.operatorframework.io/service-account-name: olm-sa
          olm.operatorframework.io/service-account-namespace: ${TEST_NAMESPACE}
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        phases:
        - name: resources
          objects:
          - ref:
              name: ${COS_NAME}-change-secret
              namespace: ${TEST_NAMESPACE}
              key: configmap
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Progressing as True with Reason Succeeded
    And ClusterObjectSet "${COS_NAME}" reports Available as True with Reason ProbesSucceeded
    And ClusterObjectSet "${COS_NAME}" has observed phase "resources" with a non-empty digest
    # Delete the immutable Secret and recreate with different content
    When resource "secret/${COS_NAME}-change-secret" is removed
    And resource is applied
      """
      apiVersion: v1
      kind: Secret
      metadata:
        name: ${COS_NAME}-change-secret
        namespace: ${TEST_NAMESPACE}
      immutable: true
      type: olm.operatorframework.io/object-data
      stringData:
        configmap: |
          {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
              "name": "test-cm-change",
              "namespace": "${TEST_NAMESPACE}"
            },
            "data": {
              "key": "TAMPERED-value"
            }
          }
      """
    And ClusterObjectSet "${COS_NAME}" reconciliation is triggered
    Then ClusterObjectSet "${COS_NAME}" reports Progressing as False with Reason Blocked and Message includes:
    """
      resolved content of 1 phase(s) has changed: phase "resources"
    """
    # Restore original content — COS should recover
    When resource "secret/${COS_NAME}-change-secret" is removed
    And resource is applied
      """
      apiVersion: v1
      kind: Secret
      metadata:
        name: ${COS_NAME}-change-secret
        namespace: ${TEST_NAMESPACE}
      immutable: true
      type: olm.operatorframework.io/object-data
      stringData:
        configmap: |
          {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
              "name": "test-cm-change",
              "namespace": "${TEST_NAMESPACE}"
            },
            "data": {
              "key": "original-value"
            }
          }
      """
    And ClusterObjectSet "${COS_NAME}" reconciliation is triggered
    Then ClusterObjectSet "${COS_NAME}" reports Progressing as True with Reason Succeeded


  @ProgressDeadline
  Scenario: Archiving a COS with ProgressDeadlineExceeded cleans up its resources
    Given min value for ClusterObjectSet .spec.progressDeadlineMinutes is set to 1
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        annotations:
          olm.operatorframework.io/service-account-name: olm-sa
          olm.operatorframework.io/service-account-namespace: ${TEST_NAMESPACE}
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        progressDeadlineMinutes: 1
        progressionProbes:
        - selector:
            type: GroupKind
            groupKind:
              group: apps
              kind: Deployment
          assertions:
          - type: ConditionEqual
            conditionEqual:
              type: Available
              status: "True"
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: test-configmap
                namespace: ${TEST_NAMESPACE}
              data:
                foo: bar
          - object:
              apiVersion: apps/v1
              kind: Deployment
              metadata:
                name: test-deployment
                namespace: ${TEST_NAMESPACE}
              spec:
                replicas: 1
                selector:
                  matchLabels:
                    app: never-ready
                template:
                  metadata:
                    labels:
                      app: never-ready
                  spec:
                    containers:
                    - name: never-ready
                      image: does-not-exist:latest
        revision: 1
      """
    Then resource "configmap/test-configmap" is installed
    And resource "deployment/test-deployment" is installed
    And ClusterObjectSet "${COS_NAME}" reports Progressing as False with Reason ProgressDeadlineExceeded
    When ClusterObjectSet "${COS_NAME}" lifecycle is set to "Archived"
    Then ClusterObjectSet "${COS_NAME}" is archived
    And resource "configmap/test-configmap" is eventually not found
    And resource "deployment/test-deployment" is eventually not found

  @ProgressDeadline
  Scenario: COS recovers from ProgressDeadlineExceeded to Succeeded when probes pass
    Given min value for ClusterObjectSet .spec.progressDeadlineMinutes is set to 1
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
    When ClusterObjectSet is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectSet
      metadata:
        annotations:
          olm.operatorframework.io/service-account-name: olm-sa
          olm.operatorframework.io/service-account-namespace: ${TEST_NAMESPACE}
        name: ${COS_NAME}
      spec:
        lifecycleState: Active
        collisionProtection: Prevent
        progressDeadlineMinutes: 1
        progressionProbes:
        - selector:
            type: GroupKind
            groupKind:
              group: apps
              kind: Deployment
          assertions:
          - type: ConditionEqual
            conditionEqual:
              type: Available
              status: "True"
        phases:
        - name: resources
          objects:
          - object:
              apiVersion: apps/v1
              kind: Deployment
              metadata:
                name: test-deployment
                namespace: ${TEST_NAMESPACE}
              spec:
                replicas: 1
                selector:
                  matchLabels:
                    app: delayed-ready
                template:
                  metadata:
                    labels:
                      app: delayed-ready
                  spec:
                    containers:
                    - name: delayed-ready
                      image: busybox:1.36
                      command: ["sleep", "1000"]
                      readinessProbe:
                        exec:
                          command: ["true"]
                        initialDelaySeconds: 65
                      securityContext:
                        runAsNonRoot: true
                        runAsUser: 1000
                        allowPrivilegeEscalation: false
                        capabilities:
                          drop:
                          - ALL
                        seccompProfile:
                          type: RuntimeDefault
        revision: 1
      """
    Then ClusterObjectSet "${COS_NAME}" reports Progressing as False with Reason ProgressDeadlineExceeded
    And ClusterObjectSet "${COS_NAME}" reports Progressing as True with Reason Succeeded
