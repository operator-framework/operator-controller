@Serial
Feature: HA failover for catalogd

  When catalogd is deployed with multiple replicas, the remaining pods must
  elect a new leader and resume serving catalogs if the leader pod is lost.

  Background:
    Given OLM is available
    And an image registry is available

  @CatalogdHA
  Scenario: Catalogd resumes serving catalogs after leader pod failure
    Given a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.0.0   | stable  |          | CRD, Deployment, ConfigMap |
    And catalogd is ready to reconcile resources
    And catalog "test" is reconciled
    When the catalogd leader pod is force-deleted
    Then a new catalogd leader is elected
    And catalog "test" reports Serving as True with Reason Available
