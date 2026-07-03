# Protecting OLMv1 API Access

By default, only cluster administrators have permission to read, create, modify, or delete `ClusterExtension` and `ClusterObjectSet` resources. No other users or groups have any access to these APIs unless explicitly granted by a cluster administrator.

Because the operator-controller runs with `cluster-admin` privileges, any user granted write access to the `ClusterExtension` API is effectively being delegated cluster-admin trust. Cluster administrators are generally discouraged from delegating this access to non-admin users. If delegation is necessary, the controls described below can help limit the scope of what delegated users can do.

## RBAC

If you must delegate `ClusterExtension` access to non-admin users, start by assigning narrowly scoped roles:

### ClusterRole
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: clusterextension-viewer-role
rules:
  - apiGroups:
      - olm.operatorframework.io
    resources:
      - clusterextensions
      - clusterobjectsets
    verbs:
      - get
      - list
      - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: clusterextension-installer-role
rules:
  - apiGroups:
      - olm.operatorframework.io
    resources:
      - clusterextensions
      - clusterobjectsets
    verbs:
      - create
      - update
      - delete
      - get
      - list
      - watch
```

### ClusterRoleBinding

Bind one of the roles above to a specific user or group. The following example grants the `clusterextension-installer-role` to user `alice` and all members of the `team-monitoring` group:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: clusterextension-installer-rolebinding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: clusterextension-installer-role
subjects:
  - kind: User
    name: alice
    apiGroup: rbac.authorization.k8s.io
  - kind: Group
    name: team-monitoring
    apiGroup: rbac.authorization.k8s.io
```

## Validating Admission Policy

A `ValidatingAdmissionPolicy` provides much more flexibility than simple RBAC when it comes to managing access to `ClusterExtensions`. For more detailed instructions on the fundamentals of the `ValidatingAdmissionPolicy` API, take a look at the Kubernetes docs [here](https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/).

In this example, we'll be creating a `ValidatingAdmissionPolicy` and accompanying `ValidatingAdmissionPolicyBinding` in order to manage `ClusterExtension` access for members of the `team-monitoring` group.

To create a `ValidatingAdmissionPolicy` for `ClusterExtensions`, begin with the following skeleton. The `matchConstraints` field tells Kubernetes which API requests the policy applies to, and `matchConditions` further narrows the scope — here, the policy only activates for users who belong to the `team-monitoring` group. Add your constraints to the `validations` list as shown in the sections below.

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: ce-policy-team-monitoring
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
      - apiGroups: ["olm.operatorframework.io"]
        apiVersions: ["v1"]
        resources: ["clusterextensions"]
        operations: ["CREATE", "UPDATE"]
  matchConditions:
    - name: only-team-monitoring
      expression: >-
        request.userInfo.groups.exists(g, g == "team-monitoring")
  validations:
    # Add validation expressions here (see sections below)
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: ce-policy-team-monitoring
spec:
  policyName: ce-policy-team-monitoring
  validationActions: ["Deny"]
```

The following custom validations can be applied through native Kubernetes' `ValidatingAdmissionPolicy` API. Each snippet shows what to add to the `validations:` list in the policy above; combine multiple entries to enforce several constraints in a single policy.

### Allow a group to manage extensions in specific namespaces

The `spec.namespace` field on a `ClusterExtension` determines which namespace the operator's resources are installed into. To restrict a group to one or more approved namespaces, add the following validation. In this example, `team-monitoring` may only target the `monitoring` or `observability` namespaces:

```yaml
  validations:
    - expression: >-
        object.spec.namespace in ["monitoring", "observability"]
      messageExpression: >-
        "spec.namespace must be one of [\"monitoring\", \"observability\"], got: \"" + object.spec.namespace + "\""
```

### Restrict which ClusterCatalogs a user/group can reference

As risk mitigation, users and groups can be restricted to only installing ClusterExtensions which target a particular catalog. In this instance, the catalog labeled as `team=monitoring` may have been vetted to contain only packages which are safe for this team to install.

```yaml
  validations:
    # Must use a catalog selector that targets team=monitoring catalogs
    - expression: >-
        has(object.spec.source.catalog) &&
        has(object.spec.source.catalog.selector) &&
        has(object.spec.source.catalog.selector.matchLabels) &&
        "team" in object.spec.source.catalog.selector.matchLabels &&
        object.spec.source.catalog.selector.matchLabels["team"] == "monitoring"
      messageExpression: >-
        "spec.source.catalog.selector.matchLabels must include team=monitoring" +
        (has(object.spec.source.catalog.selector) &&
         has(object.spec.source.catalog.selector.matchLabels) &&
         "team" in object.spec.source.catalog.selector.matchLabels
         ? ", got team=" + object.spec.source.catalog.selector.matchLabels["team"]
         : ", got: <unset>")
```

### Restrict which packages a user/group can install

If a user or group need only have access to a limited set of packages and versions, the following validation can be added to set those limits:

```yaml
  validations:
    # Package allowlist with per-package pinned minor versions
    - expression: >-
        (object.spec.source.catalog.packageName == "prometheus" &&
         has(object.spec.source.catalog.version) &&
         object.spec.source.catalog.version.matches("^v?2\\.54(\\.(0|[1-9][0-9]*))?$"))
        ||
        (object.spec.source.catalog.packageName == "alertmanager" &&
         has(object.spec.source.catalog.version) &&
         object.spec.source.catalog.version.matches("^v?0\\.27(\\.(0|[1-9][0-9]*))?$"))
      messageExpression: >-
        "package \"" + object.spec.source.catalog.packageName + "\" with version \"" +
        (has(object.spec.source.catalog.version) ? object.spec.source.catalog.version : "<unset>") +
        "\" is not allowed; permitted combinations: prometheus@2.54.x, alertmanager@0.27.x"
```

### Restrict upgrade constraint policy

The `spec.source.catalog.upgradeConstraintPolicy` field controls whether the operator-controller respects version upgrade constraints provided by the catalog. Setting it to `SelfCertified` bypasses those constraints, which can lead to unsafe upgrades. To prevent non-admin users from overriding this:

```yaml
  validations:
    - expression: >-
        !has(object.spec.source.catalog.upgradeConstraintPolicy) ||
        object.spec.source.catalog.upgradeConstraintPolicy != "SelfCertified"
      message: >-
        team-monitoring may not set upgradeConstraintPolicy to "SelfCertified".
        Only cluster-admins may bypass catalog-provided upgrade constraints.
```

### Restrict CRD upgrade safety

The `spec.install.preflight.crdUpgradeSafety` field controls whether the operator-controller runs pre-flight safety checks before applying CRD changes. Setting `enforcement` to `None` disables these checks, which can result in breaking changes being applied to cluster-wide CRDs. To prevent non-admin users from doing this:

```yaml
  validations:
    - expression: >-
        !has(object.spec.install) ||
        !has(object.spec.install.preflight) ||
        !has(object.spec.install.preflight.crdUpgradeSafety) ||
        object.spec.install.preflight.crdUpgradeSafety.enforcement != "None"
      message: >-
        team-monitoring may not disable CRD upgrade safety checks.
        Remove spec.install.preflight.crdUpgradeSafety or set enforcement to "Strict".
```

## Complete example

The following combines all of the above constraints into a single `ValidatingAdmissionPolicy` and `ValidatingAdmissionPolicyBinding`. Members of the `team-monitoring` group may only install `prometheus@2.54.x` or `alertmanager@0.27.x` into the `monitoring` namespace from catalogs labeled `team=monitoring`, and may not bypass upgrade constraints or disable CRD safety checks.

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: ce-policy-team-monitoring
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
      - apiGroups: ["olm.operatorframework.io"]
        apiVersions: ["v1"]
        resources: ["clusterextensions"]
        operations: ["CREATE", "UPDATE"]
  matchConditions:
    - name: only-team-monitoring
      expression: >-
        request.userInfo.groups.exists(g, g == "team-monitoring")
  validations:
    - expression: >-
        has(object.spec.source.catalog) &&
        has(object.spec.source.catalog.selector) &&
        has(object.spec.source.catalog.selector.matchLabels) &&
        "team" in object.spec.source.catalog.selector.matchLabels &&
        object.spec.source.catalog.selector.matchLabels["team"] == "monitoring"
      messageExpression: >-
        "spec.source.catalog.selector.matchLabels must include team=monitoring" +
        (has(object.spec.source.catalog.selector) &&
         has(object.spec.source.catalog.selector.matchLabels) &&
         "team" in object.spec.source.catalog.selector.matchLabels
         ? ", got team=" + object.spec.source.catalog.selector.matchLabels["team"]
         : ", got: <unset>")
    - expression: >-
        (object.spec.source.catalog.packageName == "prometheus" &&
         has(object.spec.source.catalog.version) &&
         object.spec.source.catalog.version.matches("^v?2\\.54(\\.(0|[1-9][0-9]*))?$"))
        ||
        (object.spec.source.catalog.packageName == "alertmanager" &&
         has(object.spec.source.catalog.version) &&
         object.spec.source.catalog.version.matches("^v?0\\.27(\\.(0|[1-9][0-9]*))?$"))
      messageExpression: >-
        "package \"" + object.spec.source.catalog.packageName + "\" with version \"" +
        (has(object.spec.source.catalog.version) ? object.spec.source.catalog.version : "<unset>") +
        "\" is not allowed; permitted combinations: prometheus@2.54.x, alertmanager@0.27.x"
    - expression: >-
        object.spec.namespace == "monitoring"
      messageExpression: >-
        "spec.namespace must be \"monitoring\", got: \"" + object.spec.namespace + "\""
    - expression: >-
        !has(object.spec.source.catalog.upgradeConstraintPolicy) ||
        object.spec.source.catalog.upgradeConstraintPolicy != "SelfCertified"
      message: >-
        team-monitoring may not set upgradeConstraintPolicy to "SelfCertified".
        Only cluster-admins may bypass catalog-provided upgrade constraints.
    - expression: >-
        !has(object.spec.install) ||
        !has(object.spec.install.preflight) ||
        !has(object.spec.install.preflight.crdUpgradeSafety) ||
        object.spec.install.preflight.crdUpgradeSafety.enforcement != "None"
      message: >-
        team-monitoring may not disable CRD upgrade safety checks.
        Remove spec.install.preflight.crdUpgradeSafety or set enforcement to "Strict".
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: ce-policy-team-monitoring
spec:
  policyName: ce-policy-team-monitoring
  validationActions: ["Deny"]
```
