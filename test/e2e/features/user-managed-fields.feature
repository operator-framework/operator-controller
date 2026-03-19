@BoxcutterRuntime
Feature: Preserve user-managed fields on deployed resources
  Fields that OLM does not declare ownership of (e.g. user-applied annotations
  and labels) belong to other managers and must be preserved across reconciliation
  cycles.

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles
    And ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}
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
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And resource "deployment/test-operator" is available

  Scenario: User-added annotations and labels coexist with bundle-defined labels after reconciliation
    # The bundle defines labels on the deployment via the CSV spec; verify they are present
    Given resource "deployment/test-operator" has labels
      | key                    | value         |
      | app.kubernetes.io/name | test-operator |
    When annotations are added to "deployment/test-operator"
      | key                           | value    |
      | example.com/custom-annotation | my-value |
    And labels are added to "deployment/test-operator"
      | key                      | value    |
      | example.com/custom-label | my-value |
    And ClusterExtension reconciliation is triggered
    And ClusterExtension has been reconciled the latest generation
    Then resource "deployment/test-operator" has annotations
      | key                            | value    |
      | example.com/custom-annotation  | my-value |
    And resource "deployment/test-operator" has labels
      | key                    | value         |
      | example.com/custom-label | my-value    |
      | app.kubernetes.io/name | test-operator |

  Scenario: Deployment rollout restart persists after OLM reconciliation
    When rollout restart is performed on "deployment/test-operator"
    Then deployment "test-operator" pod template has annotation "kubectl.kubernetes.io/restartedAt"
    And deployment "test-operator" rollout is complete
    And deployment "test-operator" has 2 replica sets
    When ClusterExtension reconciliation is triggered
    And ClusterExtension has been reconciled the latest generation
    Then deployment "test-operator" pod template has annotation "kubectl.kubernetes.io/restartedAt"
    And deployment "test-operator" rollout is complete
