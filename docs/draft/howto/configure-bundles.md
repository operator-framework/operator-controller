## Description

# Configuring OLM v1 Extensions: Deployment Configuration

In OLM v1, extensions are configured through the `ClusterExtension` resource. This guide explains how to customize operator deployments using the `deploymentConfig` option.

## Deployment Configuration

The `deploymentConfig` option allows you to customize operator deployments with environment variables, resource limits, node selectors, tolerations, and other settings. This follows the same structure as OLM v0's `Subscription.spec.config`.

### Available Fields

| Field           | Description                                           |
|:----------------|:------------------------------------------------------|
| `env`           | Environment variables to set on the operator container |
| `envFrom`       | Sources for environment variables                      |
| `resources`     | CPU/memory resource requests and limits                |
| `nodeSelector`  | Node labels for pod scheduling                         |
| `tolerations`   | Tolerations for node taints                            |
| `volumes`       | Additional volumes to mount                            |
| `volumeMounts`  | Volume mount paths for the operator container          |
| `affinity`      | Pod scheduling affinity rules                          |
| `annotations`   | Additional annotations to add to the deployment        |

### Example: Setting Environment Variables and Resource Limits

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: my-operator
spec:
  namespace: my-operator-ns
  serviceAccount:
    name: my-sa
  config:
    configType: Inline
    inline:
      deploymentConfig:
        env:
          - name: LOG_LEVEL
            value: debug
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
  source:
    sourceType: Catalog
    catalog:
      packageName: my-package
```

### Example: Node Scheduling

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: my-operator
spec:
  namespace: my-operator-ns
  serviceAccount:
    name: my-sa
  config:
    configType: Inline
    inline:
      deploymentConfig:
        nodeSelector:
          kubernetes.io/os: linux
        tolerations:
          - key: "node-role.kubernetes.io/infra"
            operator: "Exists"
            effect: "NoSchedule"
  source:
    sourceType: Catalog
    catalog:
      packageName: my-package
```

## Troubleshooting Configuration Errors

OLM v1 validates your configuration against the bundle's schema before installation proceeds. If your configuration is invalid, the `ClusterExtension` will report a `Progressing` condition with an error message.

| Error Message Example           | Cause                                        | Solution                                                 |
|:--------------------------------|:---------------------------------------------|:---------------------------------------------------------|
| `unknown field "foo"`           | You added fields not in the config schema.   | Remove unsupported fields from the inline config.        |
| `invalid type for field "..."` | A field has the wrong type (e.g., string instead of array). | Check the expected type and correct the value. |
