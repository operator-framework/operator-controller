# OPA Policies for NetworkPolicy Validation

This directory contains [Open Policy Agent (OPA)](https://www.openpolicyagent.org/) Rego policies used by [conftest](https://www.conftest.dev/) to validate generated Kubernetes manifests.

## Policy Files

### olm-networkpolicies.rego

Package: `main`

Validates core OLM NetworkPolicy requirements:

- **Deny-all policy**: Ensures a default deny-all NetworkPolicy exists with empty podSelector and both Ingress/Egress policy types
- **catalogd-controller-manager policy**: Validates the NetworkPolicy for catalogd:
  - Ingress on port 7443 (Prometheus metrics scraping)
  - Ingress on port 8443 (catalog metadata queries from operator-controller)
  - Ingress on port 9443 (Kubernetes API server webhook access)
  - General egress enabled
- **operator-controller-controller-manager policy**: Validates the NetworkPolicy for operator-controller:
  - Ingress on port 8443 (Prometheus metrics scraping)
  - General egress enabled (for pulling bundle images, connecting to catalogd, and Kubernetes API)

## Usage

These policies are automatically run as part of:

- `make lint-helm` - Validates the helm/olmv1 chart (runs `main` package)
- `make manifests` - Generates and validates core OLM manifests using `main` package policies

### Running manually

```bash
helm template olmv1 helm/olmv1 | conftest test --policy hack/conftest/policy/ --combine -n main -
```

## Adding New Policies

1. Add new rules to an existing `.rego` file or create a new one
2. Use `package main` for policies that should run by default on all manifests
3. Update the Makefile targets if new namespaces need to be included
