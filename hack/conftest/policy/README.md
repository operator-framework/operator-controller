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

### prometheus-networkpolicies.rego

Package: `prometheus`

Validates Prometheus NetworkPolicy requirements:

- Ensures a NetworkPolicy exists that allows both ingress and egress traffic for prometheus pods

## Usage

These policies are automatically run as part of:

- `make lint-helm` - Validates both helm/olmv1 and helm/prometheus charts (runs `main` and `prometheus` packages)
- `make manifests` - Generates and validates core OLM manifests using only `main` package policies 
   (Prometheus policies are intentionally skipped here, even if manifests include Prometheus resources; 
   they are validated via `make lint-helm`)

### Running manually

```bash
# Run all policies (main + prometheus namespaces)
(helm template olmv1 helm/olmv1; helm template prometheus helm/prometheus) | conftest test --policy hack/conftest/policy/ --combine -n main -n prometheus -

# Run only OLM policies
helm template olmv1 helm/olmv1 | conftest test --policy hack/conftest/policy/ --combine -n main -

# Run only prometheus policies
helm template prometheus helm/prometheus | conftest test --policy hack/conftest/policy/ --combine -n prometheus -
```

### Excluding policies

Use the `-n` (namespace) flag to selectively run policies:

```bash
# Skip prometheus policies
conftest test --policy hack/conftest/policy/ --combine -n main <input>

# Skip OLM policies
conftest test --policy hack/conftest/policy/ --combine -n prometheus <input>
```

## Adding New Policies

1. Add new rules to an existing `.rego` file or create a new one
2. Use `package main` for policies that should run by default on all manifests
3. Use a custom package name (e.g., `package prometheus`) for optional policies
4. Update the Makefile targets if new namespaces need to be included
