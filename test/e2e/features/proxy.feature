Feature: HTTPS proxy support for outbound catalog requests

  OLM's operator-controller fetches catalog data from catalogd over HTTPS.
  When HTTPS_PROXY is set in the operator-controller's environment, all
  outbound HTTPS requests must be routed through the configured proxy.

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace

  @HTTPProxy
  Scenario: operator-controller respects HTTPS_PROXY when fetching catalog data
    Given the "operator-controller" component is configured with HTTPS_PROXY "http://127.0.0.1:39999"
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
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      proxyconnect
      """

  @HTTPProxy
  Scenario: operator-controller sends catalog requests through a configured HTTPS proxy
    # The recording proxy runs on the host and cannot route to in-cluster service
    # addresses, so it responds 502 after recording the CONNECT.  This is
    # intentional: the scenario only verifies that operator-controller respects
    # HTTPS_PROXY and sends catalog fetches through the proxy, not that the full
    # end-to-end request succeeds.
    Given the "operator-controller" component is configured with HTTPS_PROXY pointing to a recording proxy
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
      """
    Then the recording proxy received a CONNECT request for the catalogd service
