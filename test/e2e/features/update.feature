Feature: Update ClusterExtension

  As an OLM user I would like to update a ClusterExtension from a catalog
  or get an appropriate information in case of an error.

  Background:
    Given OLM is available
    And an image registry is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.0.0   | alpha   |          | CRD, Deployment, ConfigMap |
      | test    | 1.0.0   | beta    |          |                            |
      | test    | 1.0.1   | beta    | 1.0.0    | CRD, Deployment, ConfigMap |
      | test    | 1.0.2   | alpha   | 1.0.0    | BadImage                   |
      | test    | 1.0.4   | beta    |          | CRD, Deployment, ConfigMap |
      | test    | 1.2.0   | beta    | 1.0.1    | CRD, Deployment, ConfigMap |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace

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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension is updated to version "1.0.1"
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.0.1" is installed in version "1.0.1"

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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.2.0
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message:
      """
      error upgrading from currently installed version "1.0.0": no bundles found for package "${PACKAGE:test}" matching version "1.2.0"
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.2.0
            upgradeConstraintPolicy: SelfCertified
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"

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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"
    And catalog "test" version "v2" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.3.0   | beta    | 1.2.0    | CRD, Deployment, ConfigMap |
    When catalog "test" is updated to version "v2"
    Then bundle "${PACKAGE:test}.1.3.0" is installed in version "1.3.0"

  Scenario: Auto update when new version becomes available in the same catalog image ref
    Given catalog "test" image version "v1" is also tagged as "latest"
    And catalog "test" is updated to version "latest"
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
    And bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"
    And catalog "test" version "v2" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.3.0   | beta    | 1.2.0    | CRD, Deployment, ConfigMap |
    When catalog "test" image version "v2" is also tagged as "latest"
    Then bundle "${PACKAGE:test}.1.3.0" is installed in version "1.3.0"

  @BoxcutterRuntime
  Scenario: Update to a version with identical bundle content creates a new revision
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
            upgradeConstraintPolicy: SelfCertified
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.0.0" is installed in version "1.0.0"
    When ClusterExtension is updated to version "1.0.4"
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.0.4" is installed in version "1.0.4"

  @BoxcutterRuntime
  Scenario: Detect collision when a second ClusterExtension installs the same package after an upgrade
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension is updated to version "1.0.1"
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.0.1" is installed in version "1.0.1"
    And the current ClusterExtension is tracked for cleanup
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}-dup
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
            version: 1.0.1
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      revision object collisions
      """

  @BoxcutterRuntime
  Scenario: Each update creates a new revision and resources not present in the new revision are removed from the cluster
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
            upgradeConstraintPolicy: SelfCertified
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension is updated to version "1.2.0"
    Then bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterExtension reports "${NAME}-2" as active revision
    And ClusterObjectSet "${NAME}-2" reports Progressing as True with Reason Succeeded
    And ClusterObjectSet "${NAME}-2" reports Available as True with Reason ProbesSucceeded
    And ClusterObjectSet "${NAME}-1" is archived
    And ClusterObjectSet "${NAME}-1" phase objects are not found or not owned by the revision

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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
            upgradeConstraintPolicy: SelfCertified
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension is updated to version "1.0.2"
    Then ClusterExtension reports "${NAME}-1, ${NAME}-2" as active revisions
    And ClusterObjectSet "${NAME}-2" reports Progressing as True with Reason RollingOut
    And ClusterObjectSet "${NAME}-2" reports Available as False with Reason ProbeFailure

  @BoxcutterRuntime
  Scenario: Changing version during stuck rollout triggers new resolution
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
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
            upgradeConstraintPolicy: SelfCertified
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension is updated to version "1.0.2"
    Then ClusterObjectSet "${NAME}-2" reports Progressing as True with Reason RollingOut
    And ClusterObjectSet "${NAME}-2" reports Available as False with Reason ProbeFailure
    And ClusterObjectSet "${NAME}-2" has annotation "olm.operatorframework.io/catalog-spec-digest" with a non-empty value
    When ClusterExtension is updated to version "1.2.0"
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "test-operator.1.2.0" is installed in version "1.2.0"
    And ClusterObjectSet "${NAME}-3" annotation "olm.operatorframework.io/catalog-spec-digest" differs from ClusterObjectSet "${NAME}-2"
