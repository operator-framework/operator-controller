Feature: Namespace PSA Management

  As an OLM user, when I install an operator that declares PSA requirements
  via the suggested-namespace-template CSV annotation, operator-controller
  should create a managed namespace with PSA labels applied.

  Background:
    Given OLM is available
    And an image registry is available

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
    And namespace "${PACKAGE:test}-system" has labels
      | key                                      | value      |
      | pod-security.kubernetes.io/enforce        | privileged |
      | pod-security.kubernetes.io/audit          | privileged |
      | pod-security.kubernetes.io/warn           | privileged |

  Scenario: User-provided namespace does not get PSA labels
    Given namespace "${TEST_NAMESPACE}" is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.0.0   | stable  |          | CRD, Deployment, ConfigMap |
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
    And namespace "${TEST_NAMESPACE}" does not have label "pod-security.kubernetes.io/enforce"
