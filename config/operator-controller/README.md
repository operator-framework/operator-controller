# OPERATOR-CONTROLLER CONFIG

## config/base

This provides an insecure (i.e. no TLS) basic configuration of operator-controller.

This configuration specifies a namespace of `olmv1-system`.

## config/overlays/cert-manager

This includes support for a secure (i.e. with TLS) configuration of operator-controller. This configuration uses:
* config/base
* config/components/tls
* config/components/ca

This configuration requires cert-manager.

## config/overlays/e2e

This provides additional configuration support for end-to-end testing, including code coverage. This configuration uses:
* config/base
* config/components/tls
* config/components/ca
* config/components/coverage

This configuration requires cert-manager.

## Components

Each of the `kustomization.yaml` files specify a `Component`, rather than an overlay.

### config/components/tls

This provides a basic configuration of operator-controller with TLS support for catalogd.

This component specifies the `olmv1-system` namespace.

This component requires cert-manager.

### config/components/coverage

Provides configuration for code coverage.

This component specifies the `olmv1-system` namespace.

### config/components/ca

Procides a CA for operator-controller operation.

This component _does not_ specify a namespace, and must be included last.

This component requires cert-manager.
