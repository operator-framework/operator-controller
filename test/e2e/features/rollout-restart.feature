@BoxcutterRuntime
Feature: Rollout Restart User Changes
  # Verifies that user-added pod template annotations persist after OLM reconciliation.
  # Fixes: https://github.com/operator-framework/operator-lifecycle-manager/issues/3392

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles
    And ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}

  Scenario: User-initiated deployment changes persist after OLM reconciliation
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
    And resource "deployment/test-operator" is available
    When user performs rollout restart on "deployment/test-operator"
    Then deployment "test-operator" has restart annotation
    And deployment "test-operator" rollout is complete
    And deployment "test-operator" has 2 replica sets
    When ClusterExtension reconciliation is triggered
    And ClusterExtension has been reconciled the latest generation
    Then deployment "test-operator" has restart annotation
    And deployment "test-operator" rollout is complete
