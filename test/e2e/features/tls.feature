Feature: TLS profile enforcement on metrics endpoints

  Background:
    Given OLM is available

  # Each scenario patches the deployment with the TLS settings under test and
  # restores the original configuration during cleanup, so scenarios are independent.

  # All three scenarios test catalogd only: the enforcement logic lives in the shared
  # tlsprofiles package, so one component is sufficient. TLS 1.2 is used for cipher
  # and curve enforcement because Go's crypto/tls does not allow the server to restrict
  # TLS 1.3 cipher suites — CipherSuites config only applies to TLS 1.2. The e2e cert
  # uses ECDSA, so ECDHE_ECDSA cipher families are required.
  @TLSProfile
  Scenario: catalogd metrics endpoint enforces configured minimum TLS version
    Given the "catalogd" deployment is configured with custom TLS minimum version "TLSv1.3"
    Then the "catalogd" metrics endpoint accepts a TLS 1.3 connection
    And the "catalogd" metrics endpoint rejects a TLS 1.2 connection

  @TLSProfile
  Scenario: catalogd metrics endpoint negotiates and enforces configured cipher suite
    Given the "catalogd" deployment is configured with custom TLS version "TLSv1.2", ciphers "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256", and curves "prime256v1"
    Then the "catalogd" metrics endpoint negotiates cipher "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256" over TLS 1.2
    And the "catalogd" metrics endpoint rejects a TLS 1.2 connection offering only cipher "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"

  @TLSProfile
  Scenario: catalogd metrics endpoint enforces configured curve preferences
    Given the "catalogd" deployment is configured with custom TLS version "TLSv1.2", ciphers "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256", and curves "prime256v1"
    Then the "catalogd" metrics endpoint accepts a TLS 1.2 connection with cipher "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256" and curve "prime256v1"
    And the "catalogd" metrics endpoint rejects a TLS 1.2 connection with cipher "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256" and only curve "secp521r1"
