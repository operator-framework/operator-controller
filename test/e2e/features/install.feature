Feature: Install ClusterExtension

  As an OLM user I would like to install a cluster extension from catalog
  or get an appropriate information in case of an error.

  Background:
    Given OLM is available
    And an image registry is available
    And namespace "${TEST_NAMESPACE}" is available

  Scenario:  Install latest available version
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


  Scenario: Report package ambiguity when multiple catalogs have the same priority
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    And a catalog "extra" with packages:
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
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      package "${PACKAGE:test}" found in multiple catalogs with the same priority
      """

  @SingleOwnNamespaceInstallSupport
  Scenario: watchNamespace config is required for extension supporting single namespace
    Given a catalog "test" with packages:
      | package                  | version | channel | replaces | contents                                        |
      | single-namespace-operator | 1.0.0   | alpha   |          | CRD, Deployment, InstallMode(SingleNamespace)    |
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
    And ClusterExtension is applied without the watchNamespace configuration
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
  Scenario: Reject watchNamespace for operator that does not support Single/OwnNamespace install modes
    Given a catalog "test" with packages:
      | package          | version | channel | replaces | contents                                                               |
      | webhook-operator | 0.0.1   | alpha   |          | StaticBundleDir(testdata/images/bundles/webhook-operator/v0.0.1)        |
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
  Scenario: Report ClusterExtension as not progressing if the rollout does not become available within given timeout
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents |
      | test    | 1.0.2   | alpha   |          | BadImage |
    And min value for ClusterExtension .spec.progressDeadlineMinutes is set to 1
    # The applier routes progressDeadlineMinutes through the ClusterObjectDeployment template; the
    # deployment enforces the rollout deadline itself, so relax its CRD minimum to accept the value of 1.
    And min value for ClusterObjectDeployment .spec.template.spec.progressDeadlineMinutes is set to 1
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
            packageName: ${PACKAGE:test}
            version: 1.0.2
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    # Post-#3055 the deployment enforces the deadline itself (its own Progressing condition); the
    # ClusterObjectSet revisions it creates do not carry progressDeadlineMinutes.
    Then ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason ProgressDeadlineExceeded
    And ClusterExtension reports Progressing as False with Reason ProgressDeadlineExceeded and Message:
      """
      latest revision exceeded its progress deadline
      """
    And ClusterExtension reports Progressing transition between 1 and 2 minutes since ClusterObjectSet "${NAME}-1" was created

  @BoxcutterRuntime
  @ProgressDeadline
  Scenario: Report ClusterExtension as not progressing if the rollout does not complete within given timeout
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents |
      | test    | 1.0.3   | alpha   |          | BadImage |
    And min value for ClusterExtension .spec.progressDeadlineMinutes is set to 1
    # The applier routes progressDeadlineMinutes through the ClusterObjectDeployment template; the
    # deployment enforces the rollout deadline itself, so relax its CRD minimum to accept the value of 1.
    And min value for ClusterObjectDeployment .spec.template.spec.progressDeadlineMinutes is set to 1
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
            packageName: ${PACKAGE:test}
            # bundle refers bad image references, so that the deployment never becomes available
            version: 1.0.3
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    # Post-#3055 the deployment enforces the deadline itself (its own Progressing condition); the
    # ClusterObjectSet revisions it creates do not carry progressDeadlineMinutes.
    Then ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason ProgressDeadlineExceeded
    And ClusterExtension reports Progressing as False with Reason ProgressDeadlineExceeded and Message:
      """
      latest revision exceeded its progress deadline
      """
    And ClusterExtension reports Progressing transition between 1 and 2 minutes since ClusterObjectSet "${NAME}-1" was created

  @BoxcutterRuntime
  Scenario:  ClusterObjectSet is annotated with bundle properties
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                                                          |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap, Property(olm.test-property=some-value) |
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
  Scenario: ClusterExtension installs via a ClusterObjectDeployment that owns the revision
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
    # The ClusterExtension records the installed bundle identity and rolls revision health up
    # onto its own conditions: Progressing (Succeeded) and Available (ProbesSucceeded, mapped
    # from the active ClusterObjectSet's Ready condition).
    And bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"
    And ClusterExtension reports Progressing as True with Reason Succeeded
    And ClusterExtension reports Available as True with Reason ProbesSucceeded
    # operator-controller installs through a ClusterObjectDeployment; the ownership chain is
    # ClusterExtension -> ClusterObjectDeployment -> ClusterObjectSet revision.
    And ClusterObjectDeployment "${NAME}" reports Available as True with Reason Available
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectDeployment "${NAME}" is owned by ClusterExtension "${NAME}"
    And ClusterObjectDeployment "${NAME}" owns 1 ClusterObjectSet
    And ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectSet "${NAME}-1" has label "olm.operatorframework.io/owner-kind" with value "ClusterExtension"
    And ClusterObjectSet "${NAME}-1" has label "olm.operatorframework.io/owner-name" with value "${NAME}"
    And ClusterObjectSet "${NAME}-1" has group "${NAME}"

  @BoxcutterRuntime
  Scenario: ClusterObjectSet objects are externalized to immutable Secrets
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
    And ClusterObjectSet "${NAME}-1" referred secrets exist in "${OLM_NAMESPACE}" namespace
    And ClusterObjectSet "${NAME}-1" referred secrets are immutable
    And ClusterObjectSet "${NAME}-1" referred secrets contain labels
      | key                                          | value   |
      | olm.operatorframework.io/owner-name          | ${NAME} |
    And ClusterObjectSet "${NAME}-1" referred secrets are owned by ClusterExtension "${NAME}"
    And ClusterObjectSet "${NAME}-1" referred secrets have type "olm.operatorframework.io/object-data"

  @DeploymentConfig
  Scenario: deploymentConfig nodeSelector is applied to the operator deployment
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.0.0" is installed in version "1.0.0"
    And resource "deployment/test-operator-${SCENARIO_ID}" is installed

  Scenario: Applying ClusterExtension with deprecated serviceAccount emits warning
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
          name: some-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension apply emits warning:
      """
      Warning: Validation failed for ValidatingAdmissionPolicy 'clusterextension-serviceaccount-deprecated' with binding 'clusterextension-serviceaccount-deprecated': spec.serviceAccount is deprecated, ignored, and will be removed in a future release. The operator-controller's cluster-admin service account is used for all cluster interactions.
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
