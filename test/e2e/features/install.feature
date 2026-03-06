Feature: Install ClusterExtension

  As an OLM user I would like to install a cluster extension from catalog
  or get an appropriate information in case of an error.

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles

  Scenario:  Install latest available version
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

  Scenario: SingleNamespace-only bundle installs in AllNamespaces mode
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
            packageName: single-namespace-operator
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And operator "single-namespace-operator" target namespace is ""

  Scenario: Install succeeds even when serviceAccount references a non-existent SA
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: non-existent-sa
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
        source:
          sourceType: Catalog
          catalog:
            packageName: test
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message:
      """
      found bundles for package "test" in multiple catalogs with the same priority [extra-catalog test-catalog]
      """

  @WebhookProviderCertManager
  Scenario: Install operator having webhooks
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
  Scenario: Report ClusterExtension as not progressing if the rollout does not become available within given timeout
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
        source:
          sourceType: Catalog
          catalog:
            packageName: test
            # bundle refers bad image references, so that the deployment never becomes available
            version: 1.0.2
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    Then ClusterExtensionRevision "${NAME}-1" reports Progressing as False with Reason ProgressDeadlineExceeded
    And ClusterExtension reports Progressing as False with Reason ProgressDeadlineExceeded and Message:
      """
      Revision has not rolled out for 1 minute(s).
      """
    And ClusterExtension reports Progressing transition between 1 and 2 minutes since its creation

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
      Revision has not rolled out for 1 minute(s).
      """
    And ClusterExtension reports Progressing transition between 1 and 2 minutes since its creation

  @BoxcutterRuntime
  Scenario:  ClusterExtensionRevision is annotated with bundle properties
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
            packageName: test
            version: 1.2.0
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    # The annotation key and value come from the bundle's metadata/properties.yaml file
    Then ClusterExtensionRevision "${NAME}-1" contains annotation "olm.properties" with value
      """
      [{"type":"olm.test-property","value":"some-value"}]
      """

  @BoxcutterRuntime
  Scenario: ClusterExtensionRevision is labeled with owner information
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
            packageName: test
            version: 1.2.0
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterExtensionRevision "${NAME}-1" has label "olm.operatorframework.io/owner-kind" with value "ClusterExtension"
    And ClusterExtensionRevision "${NAME}-1" has label "olm.operatorframework.io/owner-name" with value "${NAME}"

  @DeploymentConfig
  Scenario: deploymentConfig nodeSelector is applied to the operator deployment
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
        config:
          configType: Inline
          inline:
            deploymentConfig:
              nodeSelector:
                kubernetes.io/os: linux
        source:
          sourceType: Catalog
          catalog:
            packageName: test
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    Then resource "deployment/test-operator" matches
      """
      spec:
        template:
          spec:
            nodeSelector:
              kubernetes.io/os: linux
      """
