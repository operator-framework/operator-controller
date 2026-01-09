Feature: Install ClusterExtension

  As an OLM user I would like to install a cluster extension from catalog
  or get an appropriate information in case of an error.

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles
    And ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}

  Scenario:  Install latest available version
    When ClusterExtension is applied
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
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "test-operator.1.2.0" is installed in version "1.2.0"
    And resource "networkpolicy/test-operator-network-policy" is installed
    And resource "configmap/test-configmap" is installed
    And resource "deployment/test-operator" is installed

  @mirrored-registry
  Scenario Outline: Install latest available version from mirrored registry
    When ClusterExtension is applied
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
            packageName: <package-name>
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "<package-name>-operator.1.2.0" is installed in version "1.2.0"
    And resource "networkpolicy/test-operator-network-policy" is installed
    And resource "configmap/test-configmap" is installed
    And resource "deployment/test-operator" is installed

    Examples:
      | package-name  |
      | test-mirrored |
      | dynamic       |


  Scenario: Report that bundle cannot be installed when it exists in multiple catalogs with same priority
    Given ClusterCatalog "extra" serves bundles
    When ClusterExtension is applied
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
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message:
      """
      found bundles for package "test" in multiple catalogs with the same priority [extra-catalog test-catalog]
      """

  @SingleOwnNamespaceInstallSupport
  Scenario: watchNamespace config is required for extension supporting single namespace
    Given ServiceAccount "olm-admin" in test namespace is cluster admin
    And resource is applied
      """
      apiVersion: v1
      kind: Namespace
      metadata:
        name: single-namespace-operator-target
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
          name: olm-admin
        source:
          sourceType: Catalog
          catalog:
            packageName: single-namespace-operator
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    And ClusterExtension reports Progressing as True with Reason Retrying and Message:
      """
      error for resolved bundle "single-namespace-operator.1.0.0" with version "1.0.0":
      invalid ClusterExtension configuration: invalid configuration: required field "watchNamespace" is missing
      """
    When ClusterExtension is updated to set config.watchNamespace field
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-admin
        config:
          configType: Inline
          inline:
            watchNamespace: single-namespace-operator-target # added
        source:
          sourceType: Catalog
          catalog:
            packageName: single-namespace-operator
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    Then ClusterExtension reports Installed as True
    And bundle "single-namespace-operator.1.0.0" is installed in version "1.0.0"
    And operator "single-namespace-operator" target namespace is "single-namespace-operator-target"

  @SingleOwnNamespaceInstallSupport
  Scenario: watchNamespace config is required for extension supporting own namespace
    Given ServiceAccount "olm-admin" in test namespace is cluster admin
    And ClusterExtension is applied without the watchNamespace configuration
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-admin
        source:
          sourceType: Catalog
          catalog:
            packageName: own-namespace-operator
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    And ClusterExtension reports Progressing as True with Reason Retrying and Message:
      """
      error for resolved bundle "own-namespace-operator.1.0.0" with version
      "1.0.0": invalid ClusterExtension configuration: invalid configuration: required
      field "watchNamespace" is missing
      """
    And ClusterExtension is updated to include the watchNamespace configuration
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-admin
        config:
          configType: Inline
          inline:
            watchNamespace: some-ns # added, but not own namespace
        source:
          sourceType: Catalog
          catalog:
            packageName: own-namespace-operator
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    And ClusterExtension reports Progressing as True with Reason Retrying and Message:
      """
      error for resolved bundle "own-namespace-operator.1.0.0" with version
      "1.0.0": invalid ClusterExtension configuration: invalid configuration: 'some-ns'
      is not valid ownNamespaceInstallMode: invalid value "some-ns": watchNamespace
      must be "${TEST_NAMESPACE}" (the namespace where the operator is installed) because this
      operator only supports OwnNamespace install mode
      """
    When ClusterExtension is updated to set watchNamespace to own namespace value
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-admin
        config:
          configType: Inline
          inline:
            watchNamespace: ${TEST_NAMESPACE} # own namespace
        source:
          sourceType: Catalog
          catalog:
            packageName: own-namespace-operator
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And operator "own-namespace-operator" target namespace is "${TEST_NAMESPACE}"

  @WebhookProviderCertManager
  Scenario: Install operator having webhooks
    Given ServiceAccount "olm-admin" in test namespace is cluster admin
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-admin
        source:
          sourceType: Catalog
          catalog:
            packageName: webhook-operator
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And resource apply fails with error msg containing "Invalid value: false: Spec.Valid must be true"
      """
      apiVersion: webhook.operators.coreos.io/v1
      kind: WebhookTest
      metadata:
        name: ${NAME}
        namespace: ${TEST_NAMESPACE}
      spec:
        valid: false # webhook rejects it as invalid value
      """
    And resource is applied
      """
      apiVersion: webhook.operators.coreos.io/v1
      kind: WebhookTest
      metadata:
        name: ${NAME}
        namespace: ${TEST_NAMESPACE}
      spec:
        valid: true
      """
    And resource "webhooktest/${NAME}" matches
    """
      apiVersion: webhook.operators.coreos.io/v2
      kind: WebhookTest
      metadata:
        name: ${NAME}
        namespace: ${TEST_NAMESPACE}
      spec:
        conversion:
          valid: true
          mutate: true
      """
    And resource "webhooktest.v1.webhook.operators.coreos.io/${NAME}" matches
    """
      apiVersion: webhook.operators.coreos.io/v1
      kind: WebhookTest
      metadata:
        name: ${NAME}
        namespace: ${TEST_NAMESPACE}
      spec:
        valid: true
        mutate: true
      """

  @BoxcutterRuntime
  @ProgressDeadline
  Scenario: Report ClusterExtension as not progressing if the rollout does not complete within given timeout
    Given min value for ClusterExtension .spec.progressDeadlineMinutes is set to 1
    And min value for ClusterExtensionRevision .spec.progressDeadlineMinutes is set to 1
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        progressDeadlineMinutes: 1
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: test
            version: 1.0.3
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    Then ClusterExtensionRevision "${NAME}-1" reports Progressing as False with Reason ProgressDeadlineExceeded
    And ClusterExtension reports Progressing as False with Reason ProgressDeadlineExceeded and Message:
      """
      Revision has not rolled out for 1 minutes.
      """
    And ClusterExtension reports Progressing transition between 1 and 2 minutes since its creation
