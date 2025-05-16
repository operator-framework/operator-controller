# How To Get Your Cluster Extension RBAC Right — Working with the Preflight Permissions Check

Cluster Extensions in Operator Lifecycle Manager (OLM) v1 are installed and managed via a **service account** that you (the cluster admin) provide. Unlike OLM v0, OLM v1 itself doesn’t have cluster-admin privileges to grant Operators the access they need – **you** must ensure the service account has all necessary Role-Based Access Control (RBAC) permissions. If the service account is missing permissions, the extension’s installation will fail or hang. To address this, the **operator-controller** now performs a **preflight permissions check** before installing an extension. This check identifies any missing RBAC permissions up front and surfaces them to you so that you can fix the issues.

## Understanding the Preflight Permissions Check

When you create a `ClusterExtension` Custom Resource (CR) to install an Operator extension, the operator-controller will do a dry-run of the installation and verify that the specified service account can perform all the actions required by that extension. This includes creating all the Kubernetes objects in the bundle (Deployments, Services, CRDs, etc.), as well as creating any RBAC roles or bindings that the extension’s bundle defines.

If any required permission is missing, the preflight check will **fail fast** *before* attempting the real installation. Instead of proceeding, the operator-controller records which permissions are missing. You’ll find this information in two places:

- **ClusterExtension Status Conditions:** The `ClusterExtension` CR will have a condition (such as **Progressing** or **Installing**) with a message describing the missing permissions. The condition’s reason may be set to “Retrying” (meaning the controller will periodically retry the install) and the message will start with “pre-authorization failed: …”.
- **Operator-Controller Logs:** The same message is also logged by the operator-controller pod. If you have access to the operator-controller’s logs (in namespace `olm-controller` on OpenShift), you can see the detailed RBAC errors there as well.

### Interpreting the Preflight Check Output

The preflight check’s output enumerates the **RBAC rules** that the service account is missing. Each missing permission is listed in a structured format. For example, a message might say:

```
service account requires the following permissions to manage cluster extension:
 Namespace:"" APIGroups:[] Resources:[services] Verbs:[list,watch]
 Namespace:"pipelines" APIGroups:[] Resources:[secrets] Verbs:[get]
```

Let’s break down how to read this output:

- **`Namespace:""`** – An empty namespace in quotes means the permission is needed at the **cluster scope** (not limited to a single namespace). In the example above, `Namespace:""` for Services indicates the service account needs the ability to list/watch Services cluster-wide.
- **`APIGroups:[]`** – An empty API group (`[]`) means the **core API group** (no group). For instance, core resources like Services, Secrets, ConfigMaps have `APIGroups:[]`. If the resource is part of a named API group (e.g. `apps`, `apiextensions.k8s.io`), that group would be listed here.
- **`Resources:[...]`** – The resource type that’s missing permissions. e.g. `services`, `secrets`, `customresourcedefinitions`.
- **`Verbs:[...]`** – The specific actions (verbs) that the service account is not allowed to do for that resource. Multiple verbs listed together means none of those verbs are permitted (and are all required).

A few special cases to note:

- **Privilege Escalation Cases:** If the extension’s bundle includes the creation of a Role or ClusterRole, the service account needs to have at least the permissions it is trying to grant. If not, the preflight check will report those verbs as missing to prevent privilege escalation.
- **Missing Role References (Resolution Errors):** If an Operator’s bundle references an existing ClusterRole or Role that is not found, the preflight check will report an “authorization evaluation error” listing the missing role.

## Resolving Common RBAC Permission Errors

Once you understand what each missing permission is, the fix is usually straightforward: **grant the service account those permissions**. Here are common scenarios and how to address them:

- **Missing resource permissions (verbs):** Update or create a (Cluster)Role and RoleBinding/ClusterRoleBinding to grant the missing verbs on the resources in the specified namespaces or at cluster scope.
- **Privilege escalation missing permissions:** Treat these missing verbs as required for the installer as well, granting the service account those rights so it can create the roles it needs.
- **Missing roles/clusterroles:** Ensure any referenced roles exist by creating them or adjusting the extension’s expectations.

## Demo Scenario (OpenShift)

Below is an example demo you can run on OpenShift to see the preflight check in action:

1. **Create a minimal ServiceAccount and ClusterRole** that lacks key permissions (e.g., missing list/watch on Services and create on CRDs).
2. **Apply a ClusterExtension** pointing to the Pipelines Operator package, specifying the above service account.
3. **Describe the ClusterExtension** (`oc describe clusterextension pipelines-operator`) to see the preflight “pre-authorization failed” errors listing missing permissions.
4. **Update the ClusterRole** to include the missing verbs.
5. **Reapply the role** and observe the ClusterExtension status clear and the operator proceed with installation.

By following this guide and using the preflight output, you can iteratively grant exactly the permissions needed—no more, no less—ensuring your cluster extensions install reliably and securely.
