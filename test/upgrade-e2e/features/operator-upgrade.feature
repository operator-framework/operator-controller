Feature: Operator upgrade verification

  As an OLM developer I would like to verify that after upgrading OLM itself,
  pre-existing ClusterCatalogs and ClusterExtensions continue to function
  and can be updated.

  Background:
    Given the latest stable OLM release is installed
    And ClusterCatalog "test" serves bundles
    And ServiceAccount "olm-sa" with permissions to install extensions is available in "upgrade-ns" namespace
    And ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: upgrade-ce
      spec:
        namespace: upgrade-ns
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
    And ClusterExtension is available
    And OLM is upgraded
    And catalogd is ready to reconcile resources
    And operator-controller is ready to reconcile resources

  Scenario: ClusterCatalog continues unpacking after OLM upgrade
    When ClusterCatalog is reconciled
    Then ClusterCatalog reports Progressing as True with Reason Succeeded
    And ClusterCatalog reports Serving as True with Reason Available

  Scenario: ClusterExtension remains functional after OLM upgrade
    Given ClusterExtension is reconciled
    When ClusterExtension is updated to version "1.0.1"
    Then ClusterExtension is available
    And bundle "test-operator.1.0.1" is installed in version "1.0.1"
