Feature: Exposed various metrics

  Background:
    Given OLM is available

  Scenario Outline: component exposes metrics
    Given Service account "metrics-reader" in test namespace has permissions to fetch "<component>" metrics
    When Service account "metrics-reader" sends request to "/metrics" endpoint of "<component>" service
    Then Prometheus metrics are returned in the response

    Examples:
      | component           |
      | operator-controller |
      | catalogd            |