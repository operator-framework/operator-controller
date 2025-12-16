Feature: Exposed various metrics

  Background:
    Given OLM is available

  Scenario Outline: component exposes metrics
    Given ServiceAccount "metrics-reader" in test namespace has permissions to fetch "<component>" metrics
    When ServiceAccount "metrics-reader" sends request to "/metrics" endpoint of "<component>" service
    Then Prometheus metrics are returned in the response

    Examples:
      | component           |
      | operator-controller |
      | catalogd            |
    