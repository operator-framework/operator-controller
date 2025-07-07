## Synthetic User Permissions

!!! note
This feature is still in *alpha* the `SyntheticPermissions` feature-gate must be enabled to make use of it.
See the instructions below on how to enable it.

Synthetic user permissions enables fine-grained configuration of ClusterExtension management client RBAC permissions.
User can not only configure RBAC permissions governing the management across all ClusterExtensions, but also on a 
case-by-case basis.

### Run OLM v1with Experimental Features Enabled

```terminal title=Enable Experimental Features in a New Kind Cluster
make run-experimental
```

```terminal title=Wait for rollout to complete
kubectl rollout status -n olmv1-system deployment/operator-controller-controller-manager 
```

### How does it work?

When managing a ClusterExtension, OLM will assume the identity of user "olm:clusterextensions:<clusterextension-name>"
and group "olm:clusterextensions" limiting Kubernetes API access scope to those defined for this user and group. These
users and group do not exist beyond being defined in Cluster/RoleBinding(s) and can only be impersonated by clients with
 `impersonate` verb permissions on the `users` and `groups` resources.

### Demo

[![asciicast](https://asciinema.org/a/Jbtt8nkV8Dm7vriHxq7sxiVvi.svg)](https://asciinema.org/a/Jbtt8nkV8Dm7vriHxq7sxiVvi)

#### Examples:

##### ClusterExtension management as cluster-admin

To enable ClusterExtensions management as cluster-admin, bind the `cluster-admin` cluster role to the `olm:clusterextensions`
group:

```
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
    name: clusterextensions-group-admin-binding
roleRef:
    apiGroup: rbac.authorization.k8s.io
    kind: ClusterRole
    name: cluster-admin
subjects:
- kind: Group
  name: "olm:clusterextensions"
```

##### Scoped olm:clusterextension group + Added perms on specific extensions

Give ClusterExtension management group broad permissions to manage ClusterExtensions denying potentially dangerous
permissions such as being able to read cluster wide secrets:

```
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: clusterextension-installer
rules:
  - apiGroups: [ olm.operatorframework.io ]
    resources: [ clusterextensions/finalizers ]
    verbs: [ update ]
  - apiGroups: [ apiextensions.k8s.io ]
    resources: [ customresourcedefinitions ]
    verbs: [ create, list, watch, get, update, patch, delete ]
  - apiGroups: [ rbac.authorization.k8s.io ]
    resources: [ clusterroles, roles, clusterrolebindings, rolebindings ]
    verbs: [ create, list, watch, get, update, patch, delete ]
  - apiGroups: [""]
    resources: [configmaps, endpoints, events, pods, pod/logs, serviceaccounts, services, services/finalizers, namespaces, persistentvolumeclaims]
    verbs: ['*']
  - apiGroups: [apps]
    resources: [ '*' ]
    verbs: ['*']
  - apiGroups: [ batch ]
    resources: [ '*' ]
    verbs: [ '*' ]
  - apiGroups: [ networking.k8s.io ]
    resources: [ '*' ]
    verbs: [ '*' ]
  - apiGroups: [authentication.k8s.io]
    resources: [tokenreviews, subjectaccessreviews]
    verbs: [create]
```

```
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
    name: clusterextension-installer-binding
roleRef:
    apiGroup: rbac.authorization.k8s.io
    kind: ClusterRole
    name: clusterextension-installer
subjects:
- kind: Group
  name: "olm:clusterextensions"
```

Give a specific ClusterExtension secrets access, maybe even on specific namespaces:

```
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
    name: clusterextension-privileged
rules:
- apiGroups: [""]
  resources: [secrets]
  verbs: ['*']
```

```
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
    name: clusterextension-privileged-binding
    namespace: <some namespace>
roleRef:
    apiGroup: rbac.authorization.k8s.io
    kind: ClusterRole
    name: clusterextension-privileged
subjects:
- kind: User
  name: "olm:clusterextensions:argocd-operator"
```

Note: In this example the ClusterExtension user (or group) will still need to be updated to be able to manage
the CRs coming from the argocd operator. Some look ahead and RBAC permission wrangling will still be required.
