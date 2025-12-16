Feature: Update ClusterExtension

  As an OLM user I would like to update a ClusterExtension from a catalog
  or get an appropriate information in case of an error.

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles
    And ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}

  Scenario: Update to a successor version
    Given ClusterExtension is applied
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
    When ClusterExtension is updated to version "1.0.1"
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "test-operator.1.0.1" is installed in version "1.0.1"

  Scenario: Cannot update extension to non successor version
    Given ClusterExtension is applied
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
            version: 1.2.0
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message:
      """
      error upgrading from currently installed version "1.0.0": no bundles found for package "test" matching version "1.2.0"
      """

  Scenario: Force update to non successor version
    Given ClusterExtension is applied
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
            version: 1.2.0
            upgradeConstraintPolicy: SelfCertified
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "test-operator.1.2.0" is installed in version "1.2.0"

  @catalog-updates
  Scenario: Auto update when new version becomes available in the new catalog image ref
    Given ClusterExtension is applied
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
    And bundle "test-operator.1.2.0" is installed in version "1.2.0"
    When ClusterCatalog "test" is updated to version "v2"
    Then bundle "test-operator.1.3.0" is installed in version "1.3.0"
  
  Scenario: Auto update when new version becomes available in the same catalog image ref
    Given "test" catalog image version "v1" is also tagged as "latest"
    And ClusterCatalog "test" is updated to version "latest"
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
    And bundle "test-operator.1.2.0" is installed in version "1.2.0"
    When ClusterCatalog "test" image version "v2" is also tagged as "latest"
    Then bundle "test-operator.1.3.0" is installed in version "1.3.0"

  @BoxcutterRuntime
  Scenario: Each update creates a new revision
    Given ClusterExtension is applied
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
            upgradeConstraintPolicy: SelfCertified
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension is updated to version "1.2.0"
    Then bundle "test-operator.1.2.0" is installed in version "1.2.0"
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterExtension reports "${NAME}-2" as active revision
    And ClusterExtensionRevision "${NAME}-2" reports Progressing as True with Reason Succeeded
    And ClusterExtensionRevision "${NAME}-2" reports Available as True with Reason ProbesSucceeded
    And ClusterExtensionRevision "${NAME}-1" is archived

  @BoxcutterRuntime
  Scenario: Report all active revisions on ClusterExtension
    Given ClusterExtension is applied
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
            upgradeConstraintPolicy: SelfCertified
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension is updated to version "1.0.2"
    Then ClusterExtension reports "${NAME}-1, ${NAME}-2" as active revisions
    And ClusterExtensionRevision "${NAME}-2" reports Progressing as True with Reason Succeeded
    And ClusterExtensionRevision "${NAME}-2" reports Available as False with Reason ProbeFailure

