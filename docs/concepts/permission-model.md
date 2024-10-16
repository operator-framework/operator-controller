#### OLMv1 Permission Model

Here we aim to describe the OLMv1 permission model in terms of RBAC defined for the ClusterExtension and RBAC contained in the operator bundle ClusterServiceVersion.

The OLMv1 operator-controller generates a service account for the deployment and RBAC for the service account based on the contents of the ClusterServiceVersion in much the same way that OLMv0 does. The ClusterExtension CR also defines a service account to deploy and manage the ClusterExtension lifecycle.

For registry+v1 bundles, the deployment's service account is separate and use for managing the controller logic. Its RBAC is derived purely from the RBAC that is defined in the CSV.
The ClusterExtension service account manages the lifecycle of the bundle contents and requires RBAC that allows it to manage that lifecycle. The ClusterExtension service account needs permissions to manage the manifests that are defined by the bundle. 

Since the operator bundle contains its own RBAC, it means the ClusterExtension service account requires either:
the same set of permissions that are defined in the RBAC that it is trying to create.
bind/escalate verbs for RBAC, OR
See https://kubernetes.io/docs/reference/access-authn-authz/rbac/#privilege-escalation-prevention-and-bootstrapping

The ClusterExtension permissions are not added to the deployment.  The ClusterExtension service account and the bundle's service accounts are for different purposes. Naming conflicts between the two service accounts can lead to failure of ClusterExtension deployment.