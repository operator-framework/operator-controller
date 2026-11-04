@demo
Feature: OLM v1 Demos

  Background:
    Given OLM is available
    And catalog "operatorhubio" reports Serving as True with Reason Available

  Scenario: ClusterCatalog Quickstart
    When catalog "operatorhubio" reports Progressing as True
    Then catalog "operatorhubio" contains some packages
    And package "wavefront" in catalog "operatorhubio" has some channels defined
    And package "wavefront" in catalog "operatorhubio" has some bundles published

  @SingleOwnNamespaceInstallSupport
  Scenario: SingleNamespace Install Mode
    Given namespace "${TEST_NAMESPACE}" is available
    And namespace is applied
      """
      apiVersion: v1
      kind: Namespace
      metadata:
        name: mariadb-watch
      """
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        config:
          configType: Inline
          inline:
            watchNamespace: mariadb-watch
        source:
          sourceType: Catalog
          catalog:
            packageName: mariadb-operator
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And operator "mariadb-operator-helm-controller-manager" target namespace is "mariadb-watch"
    And rolebindings in namespace "mariadb-watch" reference service account "mariadb-operator-helm-controller-manager" in namespace "${TEST_NAMESPACE}"

  @SingleOwnNamespaceInstallSupport
  Scenario: OwnNamespace Install Mode
    Given namespace "${TEST_NAMESPACE}" is available
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        config:
          configType: Inline
          inline:
            watchNamespace: ${TEST_NAMESPACE}
        source:
          sourceType: Catalog
          catalog:
            packageName: mariadb-operator
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And operator "mariadb-operator-helm-controller-manager" target namespace is "${TEST_NAMESPACE}"

  @WebhookProviderCertManager
  Scenario: Webhook Support
    Given namespace "${TEST_NAMESPACE}" is available
    When ClusterExtension is applied
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
            packageName: telegraf-operator
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    When resource is applied
      """
      apiVersion: v1
      kind: Pod
      metadata:
        name: test-pod
        namespace: ${TEST_NAMESPACE}
        annotations:
          telegraf.influxdata.com/class: default
          telegraf.influxdata.com/inputs: |
            [[inputs.cpu]]
              percpu = false
              totalcpu = true
      spec:
        containers:
          - name: app
            image: busybox
            command: ["sleep", "3600"]
      """
    Then pod "test-pod" in test namespace has 2 containers
