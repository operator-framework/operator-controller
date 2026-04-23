Feature: Recover cluster extension from errors that might occur during its lifetime

  Background:
    Given OLM is available
    And an image registry is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |

  Scenario: Restore removed resource
    Given ServiceAccount "olm-sa" with needed permissions is available in test namespace
    And ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension is available
    And resource "configmap/test-configmap-${SCENARIO_ID}" exists
    When resource "configmap/test-configmap-${SCENARIO_ID}" is removed
    Then resource "configmap/test-configmap-${SCENARIO_ID}" is eventually restored

  Scenario: Install ClusterExtension after target namespace becomes available
    Given ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension reports Progressing as True with Reason Retrying
    When ServiceAccount "olm-sa" with needed permissions is available in test namespace
    Then ClusterExtension is available
    And ClusterExtension reports Progressing as True with Reason Succeeded

  Scenario: Install ClusterExtension after conflicting resource is removed
    Given ServiceAccount "olm-sa" with needed permissions is available in test namespace
    And resource is applied
      """
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: test-operator-${SCENARIO_ID}
        namespace: ${TEST_NAMESPACE}
      spec:
        replicas: 1
        selector:
          matchLabels:
            app: test-operator-${SCENARIO_ID}
        template:
          metadata:
            labels:
              app: test-operator-${SCENARIO_ID}
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
      """
    And ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension reports Progressing as True with Reason Retrying
    And ClusterExtension reports Installed as False
    When resource "deployment/test-operator-${SCENARIO_ID}" is removed
    Then ClusterExtension is available
    And ClusterExtension reports Progressing as True with Reason Succeeded
    And ClusterExtension reports Installed as True

  @PreflightPermissions
  Scenario: ClusterExtension installation succeeds after service account gets the required missing permissions to
    manage the bundle's resources
    Given ServiceAccount "olm-sa" is available in test namespace
    And ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      pre-authorization failed: service account requires the following permissions to manage cluster extension:
      """
    And ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      Namespace:"" APIGroups:[apiextensions.k8s.io] Resources:[customresourcedefinitions] ResourceNames:[e2e-${SCENARIO_ID}tests.e2e-${SCENARIO_ID}.e2e.operatorframework.io] Verbs:[delete,get,patch,update]
      """
    When ServiceAccount "olm-sa" with needed permissions is available in test namespace
    Then ClusterExtension is available
    And ClusterExtension reports Progressing as True with Reason Succeeded
    And ClusterExtension reports Installed as True

  # CATALOG DELETION RESILIENCE SCENARIOS

  Scenario: Auto-healing continues working after catalog deletion
    # This test proves that extensions continue to auto-heal (restore deleted resources) even when
    # their source catalog is unavailable. We verify this by:
    # 1. Deleting the catalog
    # 2. Manually deleting a managed resource (configmap)
    # 3. Verifying the resource is automatically restored
    #
    # Why this proves auto-healing works:
    # - If the controller stopped reconciling, the configmap would stay deleted
    # - Resource restoration is an observable event that PROVES active reconciliation
    # - The deployment staying healthy proves the workload continues running
    Given ServiceAccount "olm-sa" with needed permissions is available in test namespace
    And ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And resource "deployment/test-operator-${SCENARIO_ID}" is available
    And resource "configmap/test-configmap-${SCENARIO_ID}" is available
    When catalog "test" is deleted
    And resource "configmap/test-configmap-${SCENARIO_ID}" is removed
    Then resource "configmap/test-configmap-${SCENARIO_ID}" is eventually restored
    And resource "deployment/test-operator-${SCENARIO_ID}" is available

  Scenario: Spec changes are allowed when catalog is unavailable
    # This test proves that users can modify extension configuration (non-version changes) even when
    # the catalog is missing. We verify this by:
    # 1. Deleting the catalog
    # 2. Changing the preflight configuration in the ClusterExtension spec
    # 3. Verifying the controller accepts and reconciles the change successfully
    #
    # Why this proves spec changes work without catalog:
    # - If the controller rejected the change, Progressing would show Retrying or Failed
    # - Reconciliation completing (observedGeneration == generation) proves the spec was processed
    # - Progressing=Succeeded proves the controller didn't block on missing catalog
    # - Extension staying Available proves workload continues running
    Given ServiceAccount "olm-sa" with needed permissions is available in test namespace
    And ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And catalog "test" is deleted
    When ClusterExtension is updated to add preflight config
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        install:
          preflight:
            crdUpgradeSafety:
              enforcement: None
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension latest generation has been reconciled
    And ClusterExtension reports Progressing as True with Reason Succeeded
    Then ClusterExtension is available
    And ClusterExtension reports Installed as True
