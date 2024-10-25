#### OLMv1 Permission Model

Here we aim to describe the OLMv1 permission model. OLMv1 does not have permissions to manage the installation and lifecycle of cluster extensions. Rather, it requires that each cluster extension specify a service account that will be used to manage its bundle contents. The cluster extension service account permissions are a superset of the permissions specified for the service account in the operator bundle. It maintains a distinction with the operator bundle service account.

To understand the permission model, lets see the scope of the the service accounts associated with and part of the ClusterExtension deployment:

#### Service Account associated with the ClusterExtension CR

1) The ClusterExtension CR defines a service account to deploy and manage the ClusterExtension lifecycle and can be derived using the [document](../howto/derive-service-account.md). It is specified in the ClusterExtension [yaml](../tutorials/install-extension#L71) while deploying a ClusterExtension.
2) The purpose of the service account specified in the ClusterExtension spec is to manage the cluster extension lifecycle. Its permissions is the cumulative of permissions required for managing the cluster extension lifecycle and any RBAC that maybe included in the extenion bundle.
3) Since the extension bundle contains its own RBAC, it means the ClusterExtension service account requires either:
- the same set of permissions that are defined in the RBAC that it is trying to create.
- bind/escalate verbs for RBAC, see https://kubernetes.io/docs/reference/access-authn-authz/rbac/#privilege-escalation-prevention-and-bootstrapping

#### Service Account/(s) part of the Extension Bundle
1) The contents of the extension bundle may contain more service accounts and RBAC.
2) The OLMv1 operator-controller creates the service account/(s) defined as part of the extension bundle with the required RBAC for the controller business logic.


Example:

In this example, the `argocd-operator-controller-manager` [service account](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator.v0.6.0.clusterserviceversion.yaml#L1124) is created by OLMv1 when it deploys this extension bundle.

OLM v1 will assign all the RBAC specified in the extension bundle to the above service account.

The extension bundle CSV contains the [permissions](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator.v0.6.0.clusterserviceversion.yaml#L1091) and [cluster permissions](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator.v0.6.0.clusterserviceversion.yaml#L872) allow the operator to manage and run the controller logic.


Note: The ClusterExtension permissions are not propogated to the deployment. The ClusterExtension service account and the bundle's service accounts have different purposes and naming conflicts between the two service accounts can lead to failure of ClusterExtension deployment.
