# OPERATOR-CONTROLLER CONFIGURATION

The main kustomize targets are all located in the `config/overlays` directory. These are the directories that should be passed to kustomize:

e.g.
```
kustomize build config/overlays/standard > standard.yaml
```

# Overlays

All other directories are in support of of these overlays.

## config/overlays/basic-olm

This includes basic support for an insecure (non-TLS) OLMv1 deployment.

## config/overlays/standard

This includes support for a secure (i.e. with TLS) configuration of OLMv1. This configuration requires cert-manager.

This configuration is used to generate `manifests/standard.yaml`.

## config/overlays/standard-e2e

This provides additional configuration support for end-to-end testing, including code coverage. This configuration requires cert-manager.

This configuration is used to generate `manifests/standard-e2e.yaml`.

## config/overlays/prometheus

Overlay containing manifest files which enable prometheus scraping of the catalogd and operator-controller pods. Used during e2e runs to measure performance over the lifetime of the test. 

These manifests will not end up in the `manifests/` folder, as they must be applied in two distinct steps to avoid issues with applying prometheus CRDs and CRs simultaneously.

Performance alert settings can be found in: `config/overlays/prometheus/prometheus_rule.yaml`

## config/overlays/experimental

This provides additional configuration used to support experimental features, including CRDs. This configuration requires cert-manager.

This configuration is used to generate `manifests/experimental.yaml`.

## config/overlays/experimental-e2e

This provides experimental configuration and support for end-to-end testing, includng code coverage. This configuration requires cert-manager.

This configuration is used to generate `manifests/experimental-e2e.yaml`.

## config/overlays/tilt-local-dev

This provides configuration for Tilt debugging support.

# Components

Components are the kustomize configuration building blocks.

## config/components/base

This directory provides multiple configurations for organizing the base configuration into standard and experimental configurations.

:bangbang: *The following rules should be followed when configurating a feature:*

* Feature components that are GA'd and should be part of the standard manifest should be listed in `config/components/base/common/kustomization.yaml`. This `commmon` kustomization file is included by *both* the **standard** and **experimental** configurations.
* Feature components that are still experimental and should be part of the standard manifest should be listed only in `config/components/base/experimental/kustomization.yaml`.

## config/components/features

This directory contains contains configuration for features (experimental or otherwise).

:bangbang: *Feature configuration should be placed into a subdirectory here.*

## config/components/cert-manager

This directory provides configuration for using cert-manager with OLMv1.

## config/components/e2e

This directory provides configuration for end-to-end testing of OLMv1.

# Base Configuration

The `config/base` directory contains the base kubebuilder-generated configuration, along with CRDs.

# Samples

The `config/samples` directory contains example ClusterCatalog and ClusterExtension resources.
