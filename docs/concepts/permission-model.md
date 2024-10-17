#### OLMv1 Permission Model

Here we aim to describe the OLMv1 permission model. OLMv1 itself does not have permission to manage the installation and lifecycle of cluster extensions. Rather, it requires that each cluster extension specifies a service account that will be used to manage its bundle contents.


1) The purpose of the service account specified in the ClusterExtension spec, which is to manage everything in (2) below.
2) The contents of the bundle, which may contain more service accounts and RBAC. Since the operator bundle contains its own RBAC, it means the ClusterExtension service account requires either:
- the same set of permissions that are defined in the RBAC that it is trying to create.
- bind/escalate verbs for RBAC, OR
See https://kubernetes.io/docs/reference/access-authn-authz/rbac/#privilege-escalation-prevention-and-bootstrapping
4) The OLMv1 operator-controller generates a service account for the deployment and RBAC for the service account based on the contents of the ClusterServiceVersion in much the same way that OLMv0 does. The ClusterExtension CR also defines a service account to deploy and manage the ClusterExtension lifecycle

The ClusterExtension permissions are not added to the deployment.  The ClusterExtension service account and the bundle's service accounts are for different purposes. Naming conflicts between the two service accounts can lead to failure of ClusterExtension deployment.
