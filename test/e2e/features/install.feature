Feature: Install ClusterExtension

  As an OLM user I would like to install a cluster extension from catalog
  or get an appropriate information in case of an error.

  Background:
    Given OLM is available
    And an image registry is available

  Scenario:  Install latest available version
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"
    And resource "networkpolicy/test-operator-${SCENARIO_ID}-network-policy" is installed
    And resource "configmap/test-configmap-${SCENARIO_ID}" is installed
    And resource "deployment/test-operator-${SCENARIO_ID}" is installed

  @mirrored-registry
  Scenario: Install latest available version from mirrored registry
    Given a catalog "test" with packages:
      | package       | version | channel | replaces | contents                                                                                                                    |
      | test-mirrored | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap, ClusterRegistry(mirrored-registry.operator-controller-e2e.svc.cluster.local:5000) |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
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
            packageName: ${PACKAGE:test-mirrored}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test-mirrored}.1.2.0" is installed in version "1.2.0"
    And resource "networkpolicy/test-operator-${SCENARIO_ID}-network-policy" is installed
    And resource "configmap/test-configmap-${SCENARIO_ID}" is installed
    And resource "deployment/test-operator-${SCENARIO_ID}" is installed


  Scenario: Report that bundle cannot be installed when it exists in multiple catalogs with same priority
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    And a catalog "extra" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
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
            packageName: ${PACKAGE:test}
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      found bundles for package "${PACKAGE:test}" in multiple catalogs with the same priority
      """

  Scenario: Report error when ServiceAccount does not exist
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      operation cannot proceed due to the following validation error(s): service account "non-existent-sa" not found in namespace "${TEST_NAMESPACE}"
      """

  @SingleOwnNamespaceInstallSupport
  Scenario: watchNamespace config is required for extension supporting single namespace
    Given a catalog "test" with packages:
      | package                  | version | channel | replaces | contents                                        |
      | single-namespace-operator | 1.0.0   | alpha   |          | CRD, Deployment, InstallMode(SingleNamespace)    |
    And ServiceAccount "olm-admin" in test namespace is cluster admin
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
            packageName: ${PACKAGE:single-namespace-operator}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension reports Progressing as False with Reason InvalidConfiguration and Message includes:
      """
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
            packageName: ${PACKAGE:single-namespace-operator}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension reports Installed as True
    And bundle "${PACKAGE:single-namespace-operator}.1.0.0" is installed in version "1.0.0"
    And operator "test-operator-${SCENARIO_ID}" target namespace is "single-namespace-operator-target"

  @SingleOwnNamespaceInstallSupport
  Scenario: watchNamespace config is required for extension supporting own namespace
    Given a catalog "test" with packages:
      | package              | version | channel | replaces | contents                                    |
      | own-namespace-operator | 1.0.0   | alpha   |          | CRD, Deployment, InstallMode(OwnNamespace)   |
    And ServiceAccount "olm-admin" in test namespace is cluster admin
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
            packageName: ${PACKAGE:own-namespace-operator}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension reports Progressing as False with Reason InvalidConfiguration and Message includes:
      """
      invalid ClusterExtension configuration: invalid configuration: required field "watchNamespace" is missing
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
            packageName: ${PACKAGE:own-namespace-operator}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension reports Progressing as False with Reason InvalidConfiguration and Message includes:
      """
      invalid value "some-ns": must be "${TEST_NAMESPACE}"
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
            packageName: ${PACKAGE:own-namespace-operator}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And operator "test-operator-${SCENARIO_ID}" target namespace is "${TEST_NAMESPACE}"

  @WebhookProviderCertManager
  Scenario: Install operator having webhooks
    Given a catalog "test" with packages:
      | package          | version | channel | replaces | contents                                                               |
      | webhook-operator | 0.0.1   | alpha   |          | StaticBundleDir(testdata/images/bundles/webhook-operator/v0.0.1)        |
    And ServiceAccount "olm-admin" in test namespace is cluster admin
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
            packageName: ${PACKAGE:webhook-operator}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
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

  @SingleOwnNamespaceInstallSupport
  Scenario: Report failure when watchNamespace has invalid DNS-1123 name
    Given a catalog "test" with packages:
      | package                  | version | channel | replaces | contents                                        |
      | single-namespace-operator | 1.0.0   | alpha   |          | CRD, Deployment, InstallMode(SingleNamespace)    |
    And ServiceAccount "olm-admin" in test namespace is cluster admin
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
        config:
          configType: Inline
          inline:
            watchNamespace: invalid-namespace-
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:single-namespace-operator}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension reports Progressing as False with Reason InvalidConfiguration and Message includes:
      """
      invalid ClusterExtension configuration: invalid configuration: field "watchNamespace" must match pattern
      """

  @SingleOwnNamespaceInstallSupport
  @WebhookProviderCertManager
  Scenario: Reject watchNamespace for operator that does not support Single/OwnNamespace install modes
    Given a catalog "test" with packages:
      | package          | version | channel | replaces | contents                                                               |
      | webhook-operator | 0.0.1   | alpha   |          | StaticBundleDir(testdata/images/bundles/webhook-operator/v0.0.1)        |
    And ServiceAccount "olm-admin" in test namespace is cluster admin
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
        config:
          configType: Inline
          inline:
            watchNamespace: ${TEST_NAMESPACE}
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:webhook-operator}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension reports Progressing as False with Reason InvalidConfiguration and Message includes:
      """
      invalid ClusterExtension configuration: invalid configuration: unknown field "watchNamespace"
      """

  @BoxcutterRuntime
  @ProgressDeadline
  Scenario: Report ClusterExtension as not progressing if the rollout does not become available within given timeout
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents |
      | test    | 1.0.2   | alpha   |          | BadImage |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
    And min value for ClusterExtension .spec.progressDeadlineMinutes is set to 1
    And min value for ClusterObjectSet .spec.progressDeadlineMinutes is set to 1
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
            packageName: ${PACKAGE:test}
            # bundle refers bad image references, so that the deployment never becomes available
            version: 1.0.2
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterObjectSet "${NAME}-1" reports Progressing as False with Reason ProgressDeadlineExceeded
    And ClusterExtension reports Progressing as False with Reason ProgressDeadlineExceeded and Message:
      """
      Revision has not rolled out for 1 minute(s).
      """
    And ClusterExtension reports Progressing transition between 1 and 2 minutes since its creation

  @BoxcutterRuntime
  @ProgressDeadline
  Scenario: Report ClusterExtension as not progressing if the rollout does not complete within given timeout
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents |
      | test    | 1.0.3   | alpha   |          | BadImage |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
    And min value for ClusterExtension .spec.progressDeadlineMinutes is set to 1
    And min value for ClusterObjectSet .spec.progressDeadlineMinutes is set to 1
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
            packageName: ${PACKAGE:test}
            version: 1.0.3
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterObjectSet "${NAME}-1" reports Progressing as False with Reason ProgressDeadlineExceeded
    And ClusterExtension reports Progressing as False with Reason ProgressDeadlineExceeded and Message:
      """
      Revision has not rolled out for 1 minute(s).
      """
    And ClusterExtension reports Progressing transition between 1 and 2 minutes since its creation

  @BoxcutterRuntime
  Scenario:  ClusterObjectSet is annotated with bundle properties
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                                                          |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap, Property(olm.test-property=some-value) |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
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
            packageName: ${PACKAGE:test}
            version: 1.2.0
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    # The annotation key and value come from the bundle's metadata/properties.yaml file
    Then ClusterObjectSet "${NAME}-1" contains annotation "olm.properties" with value
      """
      [{"type":"olm.test-property","value":"some-value"}]
      """

  @BoxcutterRuntime
  Scenario: ClusterObjectSet is labeled with owner information
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
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
            packageName: ${PACKAGE:test}
            version: 1.2.0
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterObjectSet "${NAME}-1" has label "olm.operatorframework.io/owner-kind" with value "ClusterExtension"
    And ClusterObjectSet "${NAME}-1" has label "olm.operatorframework.io/owner-name" with value "${NAME}"

  @BoxcutterRuntime
  Scenario: ClusterObjectSet objects are externalized to immutable Secrets
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
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
            packageName: ${PACKAGE:test}
            version: 1.2.0
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterObjectSet "${NAME}-1" phase objects are managed in Kubernetes secrets
    And ClusterObjectSet "${NAME}-1" referred secrets exist in "olmv1-system" namespace
    And ClusterObjectSet "${NAME}-1" referred secrets are immutable
    And ClusterObjectSet "${NAME}-1" referred secrets contain labels
      | key                                          | value     |
      | olm.operatorframework.io/revision-name       | ${NAME}-1 |
      | olm.operatorframework.io/owner-name          | ${NAME}   |
    And ClusterObjectSet "${NAME}-1" referred secrets are owned by the object set
    And ClusterObjectSet "${NAME}-1" referred secrets have type "olm.operatorframework.io/object-data"

  @DeploymentConfig
  Scenario: deploymentConfig nodeSelector is applied to the operator deployment
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then resource "deployment/test-operator-${SCENARIO_ID}" matches
      """
      spec:
        template:
          spec:
            nodeSelector:
              kubernetes.io/os: linux
      """

  @BoxcutterRuntime
  Scenario: Install bundle with large CRD
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                       |
      | test    | 1.0.0   | beta    |          | LargeCRD(250), Deployment      |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.0.0" is installed in version "1.0.0"
    And resource "deployment/test-operator-${SCENARIO_ID}" is installed

  @BoxcutterRuntime
  @PreflightPermissions
  Scenario: Boxcutter preflight check detects missing CREATE permissions
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    And ServiceAccount "olm-sa" without create permissions is available in test namespace
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
      pre-authorization failed: service account requires the following permissions to manage cluster extension
      """
    And ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      Verbs:[create]
      """
    When ServiceAccount "olm-sa" with needed permissions is available in test namespace
    Then ClusterExtension is available
    And ClusterExtension reports Progressing as True with Reason Succeeded
    And ClusterExtension reports Installed as True
