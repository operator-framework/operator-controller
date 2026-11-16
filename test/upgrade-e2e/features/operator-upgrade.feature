Feature: Operator upgrade verification

  As an OLM developer I would like to verify that after upgrading OLM itself,
  pre-existing ClusterCatalogs and ClusterExtensions continue to function
  and can be updated.

  Background:
    Given the latest stable OLM release is installed
    And an image registry is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.0.0   | beta    |          | CRD, Deployment, ConfigMap |
      | test    | 1.0.1   | beta    | 1.0.0    | CRD, Deployment, ConfigMap |
    And ServiceAccount "olm-sa" with permissions to install extensions is available in "${TEST_NAMESPACE}" namespace
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
    And ClusterExtension is available

  # The installer and upgrader run once per suite. Run this scenario first so
  # the extension was installed by the source OLM, before the target is deployed.
  Scenario: ClusterExtension remains functional after OLM upgrade
    Given the installed extension resources are remembered
    And OLM is upgraded
    And catalogd is ready to reconcile resources
    And operator-controller is ready to reconcile resources
    And ClusterExtension is reconciled
    And the Helm migration is complete for a standard-to-experimental upgrade
    When ClusterExtension version is updated to "1.0.1"
    Then ClusterExtension is rolled out
    And bundle "${PACKAGE:test}.1.0.1" is installed in version "1.0.1"

  Scenario: ClusterCatalog continues unpacking after OLM upgrade
    Given OLM is upgraded
    And catalogd is ready to reconcile resources
    And operator-controller is ready to reconcile resources
    When catalog "test" is reconciled
    Then catalog "test" reports Progressing as True with Reason Succeeded
    And catalog "test" reports Serving as True with Reason Available
