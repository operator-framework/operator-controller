@BoxcutterRuntime
Feature: Preserve user-managed fields on deployed resources
  Fields that OLM does not declare ownership of (e.g. user-applied annotations
  and labels) belong to other managers and must be preserved across reconciliation
  cycles.

  Background:
    Given OLM is available
    And an image registry is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.2.0   | beta    |          | CRD, Deployment, ConfigMap |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace
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
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And resource "deployment/test-operator-${SCENARIO_ID}" is available

  Scenario: User-added annotations and labels coexist with bundle-defined labels after reconciliation
    # The bundle defines labels on the deployment via the CSV spec; verify they are present
    Given resource "deployment/test-operator-${SCENARIO_ID}" has labels
      | key                    | value                          |
      | app.kubernetes.io/name | test-operator-${SCENARIO_ID}   |
    When annotations are added to "deployment/test-operator-${SCENARIO_ID}"
      | key                           | value    |
      | example.com/custom-annotation | my-value |
    And labels are added to "deployment/test-operator-${SCENARIO_ID}"
      | key                      | value    |
      | example.com/custom-label | my-value |
    And ClusterExtension reconciliation is triggered
    And ClusterExtension has been reconciled the latest generation
    Then resource "deployment/test-operator-${SCENARIO_ID}" has annotations
      | key                            | value    |
      | example.com/custom-annotation  | my-value |
    And resource "deployment/test-operator-${SCENARIO_ID}" has labels
      | key                      | value                          |
      | example.com/custom-label | my-value                       |
      | app.kubernetes.io/name   | test-operator-${SCENARIO_ID}   |

  Scenario: Deployment rollout restart persists after OLM reconciliation
    When rollout restart is performed on "deployment/test-operator-${SCENARIO_ID}"
    Then deployment "test-operator-${SCENARIO_ID}" pod template has annotation "kubectl.kubernetes.io/restartedAt"
    And deployment "test-operator-${SCENARIO_ID}" rollout is complete
    And deployment "test-operator-${SCENARIO_ID}" has 2 replica sets
    When ClusterExtension reconciliation is triggered
    And ClusterExtension has been reconciled the latest generation
    Then deployment "test-operator-${SCENARIO_ID}" pod template has annotation "kubectl.kubernetes.io/restartedAt"
    And deployment "test-operator-${SCENARIO_ID}" rollout is complete
