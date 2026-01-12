Feature: RBAC Permissions for Extension Installation

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles
    And ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}

  # This test verifies that the ClusterExtension installer ServiceAccount has the necessary
  # RBAC permissions to install operators with different permission requirements.
  #
  # The rbac-escalation-operator requires permissions beyond what test-operator needs,
  # testing that the installer SA can create ClusterRoleBindings for roles with
  # permissions the SA itself doesn't directly possess (via bind/escalate verbs).
  #
  # See: docs/concepts/permission-model.md for OLMv1 permission requirements
  Scenario: Install operator with different RBAC requirements
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: rbac-escalation-test
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: rbac-escalation-operator
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    Then ClusterExtension is available
    And bundle "rbac-escalation-operator.1.0.0" is installed in version "1.0.0"

