Feature: Report status of the managed ClusterExtension workload

  As an OLM user, I would like to see reported on ClusterExtension the availability
  change of the managed workload.

  Background:
    Given OLM is available
    And an image registry is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.0.0   | alpha   |          | CRD, Deployment, ConfigMap |
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
            version: 1.0.0
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available

  @BoxcutterRuntime
  Scenario: Report availability change when managed workload is not ready
    When deployment "test-operator-${SCENARIO_ID}" reports as not ready
    Then ClusterExtension reports Available as False with Reason ProbeFailure
    And ClusterObjectSet "${NAME}-1" reports Available as False with Reason ProbeFailure

  @BoxcutterRuntime
  Scenario: Report availability change when managed workload restores its readiness
    Given deployment "test-operator-${SCENARIO_ID}" reports as not ready
    And ClusterExtension reports Available as False with Reason ProbeFailure
    And ClusterObjectSet "${NAME}-1" reports Available as False with Reason ProbeFailure
    When deployment "test-operator-${SCENARIO_ID}" reports as ready
    Then ClusterExtension is available
    And ClusterObjectSet "${NAME}-1" reports Available as True with Reason ProbesSucceeded
