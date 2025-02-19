# OPERATOR-CONTROLLER CONFIG

## config/overlays/basic-olm

This includes basic support for an insecure OLMv1 deployment. This configuration uses:
* config/base/catalogd
* config/base/operator-controller
* config/base/common

## config/overlays/cert-manager

This includes support for a secure (i.e. with TLS) configuration of OLMv1. This configuration uses:
* config/base/catalogd
* config/base/operator-controller
* config/base/common
* config/components/tls/catalogd
* config/components/tls/operator-controller
* config/components/tls/ca

This configuration requires cert-manager.

## config/overlays/e2e

This provides additional configuration support for end-to-end testing, including code coverage. This configuration uses:
* config/base/catalogd
* config/base/operator-controller
* config/base/common
* config/components/coverage
* config/components/tls/catalogd
* config/components/tls/operator-controller
* config/components/tls/ca

This configuration requires cert-manager.

## Base Configuration

The base configuration specifies a namespace of `olmv1-system`.

### config/base/catalogd

This provides the base configuration of catalogd.

### config/base/operator-controller

This provides the base configuration of operator-controller.

### config/base/common

This provides common components to both operator-controller and catalogd, i.e. namespace.

## Components

Each of the `kustomization.yaml` files specify a `Component`, rather than an overlay, and thus, can be used within the overlays.

### config/components/tls/catalogd

This provides a basic configuration of catalogd with TLS support.

This component requires cert-manager.

### config/components/tls/operator-controller

This provides a basic configuration of operator-controller with TLS support for catalogd.

This component requires cert-manager.

### config/components/tls/ca

Provides a CA for operator-controller/catalogd operation.

This component _does not_ specify a namespace, and _must_ be included last.

This component requires cert-manager.

### config/components/coverage

Provides configuration for code coverage.
