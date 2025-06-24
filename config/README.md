# OPERATOR-CONTROLLER CONFIG

## config/overlays/basic-olm

This includes basic support for an insecure OLMv1 deployment. This configuration uses:
* config/base/catalogd
* config/base/operator-controller
* config/base/common

## config/overlays/standard

This includes support for a secure (i.e. with TLS) configuration of OLMv1. This configuration uses:
* config/base/catalogd
* config/base/operator-controller
* config/base/common
* config/components/tls/catalogd
* config/components/tls/operator-controller
* config/components/tls/ca

This configuration requires cert-manager.

## config/overlays/standard-e2e

This provides additional configuration support for end-to-end testing, including code coverage. This configuration is based on **standard**, but also includes:
* config/components/e2e/coverage
* config/components/e2e/registries-conf

## config/overlays/experimental

This provides additional configuration experimental features. This configuration is based on **standard**, but also includes:
* config/components/features/*

## config/overlays/experimental-e2e

This provides additional configuration support for end-to-end testing, including code coverage, and experimental features. This configuration is based on **experimental**, but also includes:
* config/components/e2e/coverage
* config/components/e2e/registries-conf

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

### config/components/features

This is the location for feature-gated configuration.

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

### config/components/e2e/coverage

Provides code coverage configuration for end-to-end testing.

### config/components/e2e/registries-conf

Provides registry configuration for for end-to-end testing.
