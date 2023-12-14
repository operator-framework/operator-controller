# Why are we building OLM v1?

Operator Lifecycle Manager's mission has been to manage the lifecycle of cluster extensions centrally and declaratively on Kubernetes clusters. Its purpose has always been to make installing, running, and updating functional extensions to the cluster easy, safe, and reproducible for cluster administrators and PaaS administrators, throughout the lifecycle of the underlying cluster. 

OLM v0 was focused on providing unique support for these specific needs for a particular type of cluster extension, which have been coined as [operators](https://operatorhub.io/what-is-an-operator#:~:text=is%20an%20Operator-,What%20is%20an%20Operator%20after%20all%3F,or%20automation%20software%20like%20Ansible.). 
Operators are classified as one or more Kubernetes controllers, shipping with one or more API extensions (CustomResourceDefinitions) to provide additional functionality to the cluster. After running OLM v0 in production clusters for a number of years, it became apparent that there's an appetite to deviate from this coupling of CRDs and controllers, to encompass the lifecycling of extensions that are not just operators.

OLM has been helping to define lifecycles for these extensions in which the extensions

  * get installed, potentially causing other extensions to be installed as well as dependencies
  * get customized with the help of customizable configuration at runtime 
  * get upgraded to newer version/s following upgrade paths defined by extension developers 
  * and finally, get decommissioned and removed.

In the dependency model, extensions can rely on each other for required services that are out of scope of the primary purpose of an extension, allowing each extension to focus on a specific purpose. 

OLM also prevents conflicting extensions from running on the cluster, either with conflicting dependency constraints or conflicts in ownership of services provided via APIs. Since cluster extensions need to be supported with an enterprise-grade product lifecycle, there has been a growing need for allowing extension authors to limit installation and upgrade of their extension by specifying additional environmental constraints as dependencies, primarily to align with what was tested by the extension author's QE processes. In other words, there is an evergrowing ask for OLM to allow the author to enforce these support limitations in the form of additional constraints specified by extension authors in their packaging for OLM.

During their lifecycle on the cluster, OLM also manages the permissions and capabilities extensions have on the cluster as well as the permission and access tenants on the cluster have to the extensions. This is done using the Kubernetes RBAC system, in combination with tenant isolation using Kubernetes namespaces. While the interaction surface of the extensions is solely composed of Kubernetes APIs the extensions define, there is an acute need to rethink the way tenant(i.e consumers of extensions) isolation is achieved. The ask from OLM, is to provide tenant isolation in a more intuitive way than [is implemented in OLM v0](https://olm.operatorframework.io/docs/advanced-tasks/operator-scoping-with-operatorgroups/#docs)

OLM also defines a packaging model in which catalogs of extensions, usually containing the entire version history of each extension, are made available to clusters for cluster users to browse and select from. While these catalogs have so far been packaged and shipped as container images, there is a growing appetite to allow more ways of packaging and shipping these catalogs, besides also simplifying the building process of these catalogs, which so far have been very costly. The effort to bring down the cost was kicked off in OLM v0 with conversion of the underlying datastore for catalog metadata to [File-based Catalogs](https://olm.operatorframework.io/docs/reference/file-based-catalogs/), with more effort being invested to slim down the process in v1. Via new versions of extensions delivered with this new packaging system, OLM will be able to apply updates to existing running extensions on the cluster in a way where the integrity of the cluster is maintained and constraints and dependencies are kept satisfied.


For a detailed writeup of OLM v1 requirements, please read the [Product Requirements Documentation](olmv1_roadmap.md)

# The OLM community

The OLM v1 project is being tracked in a [GitHub project](https://github.com/orgs/operator-framework/projects/8/)

You can reach out to the OLM community for feedbacks/discussions/contributions in the following channels:

  * Kubernetes slack channel: [#olm-dev](https://kubernetes.slack.com/messages/olm-dev)
  * [Operator Framework on Google Groups](https://groups.google.com/forum/#!forum/operator-framework)
