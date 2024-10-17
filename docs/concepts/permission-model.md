#### OLMv1 Permission Model

Here we aim to describe the OLMv1 permission model. OLMv1 does not have permissions to manage the installation and lifecycle of cluster extensions. Rather, it requires that each cluster extension specifies a service account that will be used to manage its bundle contents. The cluster extension service account is a superset of the permissions specified for the service account in the operator bundle. It maintains a distinction with the operator bundle service account.


1) The purpose of the service account specified in the ClusterExtension spec, which is to manage everything in (2) below.
2) The contents of the bundle, which may contain more service accounts and RBAC. Since the operator bundle contains its own RBAC, it means the ClusterExtension service account requires either:
- the same set of permissions that are defined in the RBAC that it is trying to create.
- bind/escalate verbs for RBAC, OR
See https://kubernetes.io/docs/reference/access-authn-authz/rbac/#privilege-escalation-prevention-and-bootstrapping
3) The OLMv1 operator-controller generates a service account for the deployment and RBAC for the service account based on the contents of the ClusterServiceVersion in much the same way that OLMv0 does. In the ArgoCD example, the [controller service account](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator.v0.6.0.clusterserviceversion.yaml#L1124) permissions allow the operator to manage and run the controller logic. 
4) The ClusterExtension CR also defines a service account to deploy and manage the ClusterExtension lifecycle and can be derived using the [document](../howto/dervice-service-account.md). It is specified in the ClusterExtension [yaml](../tutorials/install-extension#L71) while deploying a ClusterExtension.

Note: The ClusterExtension permissions are not propogated to the deployment. The ClusterExtension service account and the bundle's service accounts have different purposes and naming conflicts between the two service accounts can lead to failure of ClusterExtension deployment.
