# Configuring a service account when the cluster uses the `OwnerReferencesPermissionEnforcement` admission plugin

The [`OwnerReferencesPermissionEnforcement`](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#ownerreferencespermissionenforcement) admission plugin requires a user to have permission to set finalizers on owner objects when creating or updating an object to contain an `ownerReference` with `blockOwnerDeletion: true`.

When operator-controller installs or upgrades a `ClusterExtension`, it sets an `ownerReference` on each object with `blockOwnerDeletion: true`. Therefore, serviceaccounts configured in `.spec.serviceAccount.name` must have the following permission in a bound `ClusterRole`:

   ```yaml
   - apiGroups: ["olm.operatorframework.io"]
     resources: ["clusterextensions/finalizers"]
     verbs: ["update"]
     resourceNames: ["<clusterExtensionName>"]
   ```

