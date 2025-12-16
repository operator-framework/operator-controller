Feature: Report status of the managed ClusterExtension workload

  As an OLM user, I would like to see reported on ClusterExtension the availability
  change of the managed workload.

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
            version: 1.0.0
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available

  @BoxcutterRuntime
  Scenario: Report availability change when managed workload is not ready
    When resource "deployment/test-operator" reports as not ready
    Then ClusterExtension reports Available as False with Reason ProbeFailure
    And ClusterExtensionRevision "${NAME}-1" reports Available as False with Reason ProbeFailure

  @BoxcutterRuntime
  Scenario: Report availability change when managed workload restores its readiness
    Given resource "deployment/test-operator" reports as not ready
    And ClusterExtension reports Available as False with Reason ProbeFailure
    And ClusterExtensionRevision "${NAME}-1" reports Available as False with Reason ProbeFailure
    When resource "deployment/test-operator" reports as ready
    Then ClusterExtension is available
    And ClusterExtensionRevision "${NAME}-1" reports Available as True with Reason ProbesSucceeded