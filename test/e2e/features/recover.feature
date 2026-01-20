Feature: Recover cluster extension from errors that might occur during its lifetime

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles

  Scenario: Restore removed resource
    Given ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}
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
            packageName: test
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    And ClusterExtension is available
    And resource "configmap/test-configmap" exists
    When resource "configmap/test-configmap" is removed
    Then resource "configmap/test-configmap" is eventually restored

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
            packageName: test
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    And ClusterExtension reports Progressing as True with Reason Retrying
    When ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}
    Then ClusterExtension is available
    And ClusterExtension reports Progressing as True with Reason Succeeded

  Scenario: Install ClusterExtension after conflicting resource is removed
    Given ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}
    And resource is applied
      """
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: test-operator
        namespace: ${TEST_NAMESPACE}
      spec:
        replicas: 1
        selector:
          matchLabels:
            app: test-operator
        template:
          metadata:
            labels:
              app: test-operator
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
            packageName: test
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    And ClusterExtension reports Progressing as True with Reason Retrying
    And ClusterExtension reports Installed as False
    When resource "deployment/test-operator" is removed
    Then ClusterExtension is available
    And ClusterExtension reports Progressing as True with Reason Succeeded
    And ClusterExtension reports Installed as True

  @PreflightPermissions
  Scenario: ClusterExtension installation succeeds after service account gets the required missing permissions to
    manage the bundle's resources
    Given ServiceAccount "olm-sa" is available in ${TEST_NAMESPACE}
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
            packageName: test
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    And ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      error for resolved bundle "test-operator.1.2.0" with version "1.2.0": creating new Revision: pre-authorization failed: service account requires the following permissions to manage cluster extension:
      """
    And ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      Namespace:"" APIGroups:[apiextensions.k8s.io] Resources:[customresourcedefinitions] ResourceNames:[olme2etests.olm.operatorframework.io] Verbs:[delete,get,patch,update]
      """
    When ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}
    Then ClusterExtension is available
    And ClusterExtension reports Progressing as True with Reason Succeeded
    And ClusterExtension reports Installed as True
