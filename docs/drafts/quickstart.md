# Quickstart Guide

## Installation

> [!CAUTION]  
> Operator-Controller depends on [cert-manager](https://cert-manager.io/). Running the following command
> may affect an existing installation of cert-manager and cause cluster instability.

> [!TIP]
> You can easily provision a local cluster with [Kind](https://kind.sigs.k8s.io/)

To install the latest release of OLMv1, execute the following command:

```bash
curl -L -s https://github.com/operator-framework/operator-controller/releases/latest/download/install.sh | bash -s
```