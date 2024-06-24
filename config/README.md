# OPERATOR-CONTROLLER CONFIG

## config/base

This provides an insecure (i.e. no TLS) basic configuration of operator-controller.

This configuration specifies a namespace of `olmv1-system`.

## config/secure

This includes support for a secure (i.e. with TLS) configuration of operator-controller. This configuration uses:
* config/base
* config/overlays/tls
* config/overlays/ca

This configuration requires cert-manager.

## config/e2e

This provides additional configuration support for end-to-end testing, including code coverage. This configuration uses:
* config/base
* config/overlays/tls
* config/overlays/ca
* config/overlays/coverage

This configuration requires cert-manager.

## Overlays

###  config/overlays/tls

This provides a basic configuration of operator-controller with TLS support for catalogd.

This configuration specifies the `olmv1-system` namespace.

This configuration requires cert-manager.

### config/overlays/coverage

Provides configuration for code coverage.

This configuration specifies the `olmv1-system` namespace.

### config/overlays/ca

Procides a CA for operator-controller operation.

This configuration specifies the the `cert-manager` namespace for the CA components.

This configuration requires cert-manager.
