# NetworkPolicy in OLMv1

## Overview

OLMv1 uses [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/) to secure communication between components, restricting network traffic to only what's necessary for proper functionality. 

* The catalogd NetworkPolicy is implemented [here](https://github.com/operator-framework/operator-controller/blob/main/helm/olmv1/templates/networkpolicy/networkpolicy-olmv1-system-catalogd-controller-manager.yml).
* The operator-controller is implemented [here](https://github.com/operator-framework/operator-controller/blob/main/helm/olmv1/templates/networkpolicy/networkpolicy-olmv1-system-operator-controller-controller-manager.yml).

This document explains the details of `NetworkPolicy` implementation for the core components.


## Implementation Overview

NetworkPolicy is implemented for both catalogd and operator-controller components to:

* Restrict incoming (ingress) traffic to only required ports and services
* Control outgoing (egress) traffic patterns

Each component has a dedicated NetworkPolicy that applies to its respective pod through label selectors:

* For catalogd: `app.kubernetes.io/name=catalogd`
* For operator-controller: `app.kubernetes.io/name=operator-controller`

### Catalogd NetworkPolicy

- Ingress Rules
Catalogd exposes three services, and its NetworkPolicy allows ingress traffic to the following TCP ports:

* 7443: Metrics server for Prometheus metrics
* 8443: Catalogd HTTPS server for catalog metadata API
* 9443: Webhook server for Mutating Admission Webhook implementation

All other ingress traffic to the catalogd pod is blocked.

- Egress Rules
Catalogd needs to communicate with:

* The Kubernetes API server
* Image registries specified in ClusterCatalog objects

Currently, all egress traffic from catalogd is allowed, to support communication with arbitrary image registries that aren't known at install time.

### Operator-Controller NetworkPolicy

- Ingress Rules
Operator-controller exposes one service, and its NetworkPolicy allows ingress traffic to:

* 8443: Metrics server for Prometheus metrics

All other ingress traffic to the operator-controller pod is blocked.

- Egress Rules
Operator-controller needs to communicate with:

* The Kubernetes API server
* Catalogd's HTTPS server (on port 8443)
* Image registries specified in bundle metadata

Currently, all egress traffic from operator-controller is allowed to support communication with arbitrary image registries that aren't known at install time.

## Security Considerations

The current implementation focuses on securing ingress traffic while allowing all egress traffic. This approach:

* Prevents unauthorized incoming connections
* Allows communication with arbitrary image registries
* Establishes a foundation for future refinements to egress rules

While allowing all egress does present some security risks, this implementation provides significant security improvements over having no network policies at all.

## Troubleshooting Network Issues

If you encounter network connectivity issues after deploying OLMv1, consider the following:

* Verify NetworkPolicy support: Ensure your cluster has a CNI plugin that supports NetworkPolicy. If your Kubernetes cluster is using a Container Network Interface (CNI) plugin that doesn't support NetworkPolicy, then the NetworkPolicy resources you create will be completely ignored and have no effect whatsoever on traffic flow.
* Check pod labels: Confirm that catalogd and operator-controller pods have the correct labels for NetworkPolicy selection:

```bash
# Verify catalogd pod labels
kubectl get pods -n olmv1-system --selector=apps.kubernetes.io/name=catalogd

# Verify operator-controller pod labels
kubectl get pods -n olmv1-system --selector=apps.kubernetes.io/name=operator-controller

# Compare with actual pod names
kubectl get pods -n olmv1-system | grep -E 'catalogd|operator-controller'
```
* Inspect logs: Check component logs for connection errors

For more comprehensive information on NetworkPolicy, see: 

- How NetworkPolicy is implemented with [network plugins](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/network-plugins/) via the Container Network Interface (CNI)
- Installing [Network Policy Providers](https://kubernetes.io/docs/tasks/administer-cluster/network-policy-provider/) documentation.
