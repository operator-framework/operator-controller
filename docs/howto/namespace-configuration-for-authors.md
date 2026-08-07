# Namespace Configuration for Bundle Authors

Bundle authors can specify their preferred namespace configuration through CSV annotations. These annotations are used by operator-controller when the cluster admin does not provide an explicit `spec.namespace`.

## Annotations

### `operatorframework.io/suggested-namespace-template`

Full namespace template with metadata. Use this when your operator needs specific labels or annotations on its namespace (e.g., PSA labels).

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: ClusterServiceVersion
metadata:
  name: my-operator.v1.0.0
  annotations:
    operatorframework.io/suggested-namespace-template: |
      {
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {
          "name": "my-operator-system",
          "labels": {
            "pod-security.kubernetes.io/enforce": "privileged",
            "pod-security.kubernetes.io/audit": "privileged",
            "pod-security.kubernetes.io/warn": "privileged"
          }
        }
      }
```

### `operators.operatorframework.io/suggested-namespace`

Simple namespace name without metadata. Use this when you want a specific name but don't need labels or annotations.

```yaml
annotations:
  operators.operatorframework.io/suggested-namespace: my-operator-system
```

### No annotation

If neither annotation is present, operator-controller uses `<packageName>-system` as the namespace name.

## Priority

If both annotations are present, `suggested-namespace-template` takes priority.

## Guidelines

- Always include PSA labels if your operator runs privileged containers.
- Use a descriptive, unique namespace name that includes your package name to avoid collisions.
- Do not assume the namespace name will be exactly what you suggest as cluster admins can override it by setting `spec.namespace`.
- The namespace name from the template is used only when `spec.namespace` is omitted. When set, the admin's choice takes precedence and no namespace object is created.

## Consistency across bundle formats

The `operatorframework.io/suggested-namespace-template` and `operators.operatorframework.io/suggested-namespace` annotations are the canonical way to declare namespace preferences. Future bundle formats should use the same annotation keys to avoid divergence across the ecosystem.
