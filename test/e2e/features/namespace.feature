Feature: Namespace PSA Management

  As an OLM user, when I install an operator that declares PSA requirements
  via the suggested-namespace-template CSV annotation, operator-controller
  should create a managed namespace with PSA labels applied and leave
  user-provided namespaces unmanaged.

  Background:
    Given OLM is available
    And an image registry is available

  @BoxcutterRuntime
  Scenario: Managed namespace with PSA template applies labels
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                                |
      | test    | 1.0.0   | stable  |          | CRD, Deployment, NSTemplate(privileged) |
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
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
    And namespace "${PACKAGE:test}-system" is managed by OLM
    And namespace "${PACKAGE:test}-system" has labels
      | key                                      | value      |
      | pod-security.kubernetes.io/enforce        | privileged |
      | pod-security.kubernetes.io/audit          | privileged |
      | pod-security.kubernetes.io/warn           | privileged |
      | e2e.olm.operatorframework.io/template     | applied    |

  Scenario: User-provided namespace remains unmanaged
    Given namespace "${TEST_NAMESPACE}" is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                                           |
      | test    | 1.0.0   | stable  |          | CRD, Deployment, ConfigMap, NSTemplate(privileged) |
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
    # Use a custom template label because other cluster controllers may apply PSA labels.
    And namespace "${TEST_NAMESPACE}" does not have label "e2e.olm.operatorframework.io/template"
    And namespace "${TEST_NAMESPACE}" has no owner references
