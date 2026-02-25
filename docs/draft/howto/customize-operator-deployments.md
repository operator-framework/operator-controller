# How to Customize Operator Deployments with DeploymentConfig

## Description

!!! note
    This feature is still in `alpha` and the `DeploymentConfig` feature gate must be enabled to make use of it.
    See the instructions below on how to enable it.

---

The Bundle Deployment Configuration feature allows you to customize how operators are deployed in your cluster by configuring deployment-level settings such as resource requirements, node placement, environment variables, storage, and annotations. This provides feature parity with OLM v0's `Subscription.spec.config` (SubscriptionConfig) functionality.

This is particularly useful for:
- **Targeted Scheduling**: Control operator pod placement using node selectors, affinity rules, and tolerations
- **Custom Environments**: Modify operator behavior for different deployment contexts using environment variables
- **Resource Management**: Set precise CPU and memory allocation requirements
- **Flexible Storage**: Attach custom storage volumes to operator pods
- **Operational Metadata**: Add custom annotations to deployments and pods

## Enabling the Feature Gate

To enable the Bundle Deployment Configuration feature gate, you need to patch the `operator-controller-controller-manager` deployment in the `olmv1-system` namespace. This will add the `--feature-gates=DeploymentConfig=true` argument to the manager container.

1. **Patch the deployment:**

    ```bash
    kubectl patch deployment -n olmv1-system operator-controller-controller-manager --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--feature-gates=DeploymentConfig=true"}]'
    ```

2. **Wait for the controller manager pods to be ready:**

    ```bash
    kubectl -n olmv1-system wait --for=condition=ready pods -l app.kubernetes.io/name=operator-controller
    ```

Once the above wait condition is met, the `DeploymentConfig` feature gate should be enabled in operator-controller.

## Configuring Operator Deployments

Deployment customizations are specified in the `spec.config.inline` field of a ClusterExtension resource, which accepts a JSON object. Within this object, you can include a `deploymentConfig` key with your deployment customization settings. The configuration structure follows the same format as OLM v0's SubscriptionConfig.

### Basic Example

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: my-operator
spec:
  namespace: my-namespace
  serviceAccount:
    name: my-operator-sa
  source:
    sourceType: Catalog
    catalog:
      packageName: my-operator
  config:
    inline:
      deploymentConfig:
        # Add environment variables
        env:
          - name: LOG_LEVEL
            value: "debug"
        # Set resource requirements
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "200m"
```

## Configuration Options

### Environment Variables

Add or override environment variables in operator containers:

```yaml
config:
  inline:
    deploymentConfig:
      env:
        - name: LOG_LEVEL
          value: "debug"
        - name: ENABLE_WEBHOOKS
          value: "true"
      envFrom:
        - configMapRef:
            name: operator-config
        - secretRef:
            name: operator-secrets
```

**Behavior**: Environment variables specified in the `env` list are merged with existing container environment variables. If a variable with the same name exists, the `deploymentConfig` value takes precedence. Variables from `envFrom` are appended to the existing list.

### Resource Requirements

Control CPU and memory allocation:

```yaml
config:
  inline:
    deploymentConfig:
      resources:
        requests:
          memory: "256Mi"
          cpu: "100m"
        limits:
          memory: "512Mi"
          cpu: "200m"
```

**Behavior**: Completely replaces any existing resource requirements defined in the bundle.

### Node Placement

#### Node Selector

Schedule operator pods on specific nodes:

```yaml
config:
  inline:
    deploymentConfig:
      nodeSelector:
        infrastructure: "dedicated"
        disktype: "ssd"
```

**Behavior**: Completely replaces any existing nodeSelector defined in the bundle.

#### Tolerations

Allow operator pods to be scheduled on tainted nodes:

```yaml
config:
  inline:
    deploymentConfig:
      tolerations:
        - key: "dedicated"
          operator: "Equal"
          value: "operators"
          effect: "NoSchedule"
        - key: "gpu"
          operator: "Exists"
          effect: "NoSchedule"
```

**Behavior**: Tolerations are appended to any existing tolerations defined in the bundle.

#### Affinity

Control pod placement with affinity rules:

```yaml
config:
  inline:
    deploymentConfig:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: kubernetes.io/arch
                    operator: In
                    values:
                      - amd64
                      - arm64
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchExpressions:
                    - key: app
                      operator: In
                      values:
                        - my-operator
                topologyKey: kubernetes.io/hostname
```

**Behavior**: Selectively overrides affinity attributes. Non-nil fields in the `affinity` configuration replace the corresponding fields in the bundle's affinity configuration.

### Storage

Add custom volumes and volume mounts:

```yaml
config:
  inline:
    deploymentConfig:
      volumes:
        - name: cache-volume
          emptyDir:
            sizeLimit: 1Gi
        - name: config-volume
          configMap:
            name: operator-config
      volumeMounts:
        - name: cache-volume
          mountPath: /var/cache
        - name: config-volume
          mountPath: /etc/config
          readOnly: true
```

**Behavior**: Volumes and volumeMounts are appended to any existing volumes/volumeMounts defined in the bundle.

### Annotations

Add custom annotations to deployment and pod templates:

```yaml
config:
  inline:
    deploymentConfig:
      annotations:
        monitoring.io/scrape: "true"
        monitoring.io/port: "8080"
        custom.annotation/key: "value"
```

**Behavior**: Annotations are merged with existing annotations. If the same annotation key exists in both the bundle and the configuration, the bundle's annotation value takes precedence.

## Complete Example

Here's a comprehensive example that demonstrates multiple configuration options:

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: production-operator
spec:
  namespace: production-operators
  serviceAccount:
    name: production-operator-installer
  source:
    sourceType: Catalog
    catalog:
      packageName: production-operator
      version: 1.2.3
  config:
    inline:
      # Combined with deploymentConfig for operators that support namespace scoping
      watchNamespace: "production-workloads"

      deploymentConfig:
        # Schedule on dedicated operator nodes
        nodeSelector:
          node-role.kubernetes.io/operator: ""

        # Tolerate the operator node taint
        tolerations:
          - key: "node-role.kubernetes.io/operator"
            operator: "Exists"
            effect: "NoSchedule"

        # Set resource requirements
        resources:
          requests:
            memory: "512Mi"
            cpu: "250m"
          limits:
            memory: "1Gi"
            cpu: "500m"

        # Configure environment
        env:
          - name: LOG_LEVEL
            value: "info"
          - name: ENABLE_METRICS
            value: "true"
          - name: METRICS_PORT
            value: "8080"
        envFrom:
          - secretRef:
              name: operator-credentials

        # Add cache volume
        volumes:
          - name: operator-cache
            emptyDir:
              sizeLimit: 2Gi
        volumeMounts:
          - name: operator-cache
            mountPath: /var/cache/operator

        # Add monitoring annotations
        annotations:
          prometheus.io/scrape: "true"
          prometheus.io/port: "8080"
          prometheus.io/path: "/metrics"

        # Prefer spreading across zones
        affinity:
          podAntiAffinity:
            preferredDuringSchedulingIgnoredDuringExecution:
              - weight: 100
                podAffinityTerm:
                  labelSelector:
                    matchLabels:
                      olm.operatorframework.io/owner-kind: ClusterExtension
                      olm.operatorframework.io/owner-name: production-operator
                  topologyKey: topology.kubernetes.io/zone
```

## Migration from OLM v0

If you're migrating from OLM v0, you can directly transfer your `Subscription.spec.config` settings to the `deploymentConfig` object within `ClusterExtension.spec.config.inline`. The structure is identical.

### OLM v0 Example

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: my-operator
  namespace: operators
spec:
  channel: stable
  name: my-operator
  source: operatorhubio-catalog
  sourceNamespace: olm
  config:
    env:
      - name: LOG_LEVEL
        value: "debug"
    resources:
      requests:
        memory: "256Mi"
        cpu: "100m"
```

### OLM v1 Equivalent

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: my-operator
spec:
  namespace: operators
  serviceAccount:
    name: my-operator-installer
  source:
    sourceType: Catalog
    catalog:
      packageName: my-operator
      channel: stable
  config:
    inline:
      deploymentConfig:
        env:
          - name: LOG_LEVEL
            value: "debug"
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
```

## Validation

When you apply a ClusterExtension with `deploymentConfig` in the `inline` configuration object, OLM v1 validates the configuration against a JSON schema.

### How the Schema is Generated (Registry+v1 Bundles)

For registry+v1 bundles, the validation schema is automatically generated from the official Kubernetes API definitions to ensure accuracy and consistency:

1. **Source**: The schema is based on the `v1alpha1.SubscriptionConfig` type from `github.com/operator-framework/api`, the same type used in OLM v0
2. **Schema Generation**: A tool parses the SubscriptionConfig struct and maps each field to official Kubernetes OpenAPI v3 schema definitions
3. **Kubernetes API Schemas**: Field types like `ResourceRequirements`, `Toleration`, `Affinity`, etc. are validated against the official Kubernetes schema specifications, ensuring the same validation rules apply as in native Kubernetes resources
4. **Exclusions**: The `selector` field from v0's SubscriptionConfig is excluded because it was never implemented in OLM v0
5. **Frozen Snapshot**: The generated schema is stored as a frozen snapshot in the operator-controller codebase, providing stability between releases

This approach guarantees that for registry+v1 bundles:
- Configuration validation matches official Kubernetes API validation
- There is perfect parity with OLM v0's SubscriptionConfig structure
- New fields added to the upstream SubscriptionConfig type can be automatically incorporated when the operator-controller dependency is updated

### Validation Errors

If the configuration is invalid, the ClusterExtension will report a `Progressing` condition with details about the validation error.

Common validation errors:

| Error | Cause | Solution |
|:------|:------|:---------|
| `unknown field "deploymentConfig"` | The `DeploymentConfig` feature gate is not enabled | Enable the feature gate using the instructions above |
| `invalid value for field "X"` | The field value doesn't match the expected type or format | Check the field type in the examples above |
| `required field "Y" is missing` | A required nested field is missing | Add the required field with an appropriate value |

## Verify Deployment Customization

After applying your ClusterExtension with deployment configuration, you can verify that the customizations were applied:

1. **Check ClusterExtension status:**

    ```bash
    kubectl get clusterextension my-operator -o yaml
    ```

    Look for the `Installed` condition to be `True`.

2. **Inspect the generated deployment:**

    ```bash
    kubectl get deployment -n my-namespace -l olm.operatorframework.io/owner-name=my-operator -o yaml
    ```

    Verify that your custom environment variables, resource requirements, node selectors, tolerations, volumes, and annotations are present in the deployment spec.

3. **Check pod configuration:**

    ```bash
    kubectl get pods -n my-namespace -l olm.operatorframework.io/owner-name=my-operator -o yaml
    ```

    Confirm that pods are running with the expected configuration.

## Notes

- **Feature Parity**: This feature provides complete feature parity with OLM v0's `SubscriptionConfig`, with the exception of the `selector` field, which was never implemented in OLM v0 and is ignored in both versions.
- **Merge Behavior**: Different configuration fields have different merge behaviors (replace, merge, or append). See the individual sections above for details.
- **Validation Timing**: Configuration is validated when the ClusterExtension is created or updated. Invalid configurations will prevent installation.
- **Updates**: Changes to the `deploymentConfig` object will trigger a reconciliation and update the operator deployment accordingly.
- **JSON Object**: The `spec.config.inline` field accepts a JSON object. The `deploymentConfig` key is one of the supported keys within this object, alongside others like `watchNamespace`.
