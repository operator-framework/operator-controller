OLM Personas

# About this doc
This document attempts to identify essential roles in the OLM lifecycle and associate the duties logically performed by each role. Though some roles can be (and even may typically be) performed by the same actor, they are logically distinct roles with different goals.

# Framework
OLM roles are broadly categorized here as **Producers** or **Consumers**, depicting whether that role typically is producing content for use in the ecosystem or is using (consuming) content. 

# Terminology
Some terminology exists outside of this document in different contexts, so where a common term has a specific meaning in the context of OLMv1, it is noted below.  Where possible, generic terms are used instead of, for e.g. specific to registry+v1.
- **extension**:  a representation of any OLMv1 installable
- **FBC**: [file-based catalog](https://olm.operatorframework.io/docs/reference/file-based-catalogs/), a composite YAML schema for expressing extensions and their related upgrade graphs


# Consumers
## Cluster Admin
*Who is it?*

This role encompasses the basic full-permissions-required creation/maintenance of a cluster, and any non-OLM-ecosystem activities, such as creating, scaling, and upgrading a cluster.

*What does it do?*

- Creates cluster
- Scales cluster
- Miscellaneous Cluster Administration
- Upgrades cluster

## Cluster Extension Admin
*Who is it?*

This role encompasses privileged operations required for OLMv1 and associated operators to deploy workloads to the cluster. This role may exist as a set of activities executed by a cluster admin, but also may operate independently of that role, depending on the necessary privileges.

*What does it do?*

- Creates enabling infrastructure for extension lifecycle (service accounts, etc.)
- Installs extensions
- Upgrades extensions
- Removes extensions
- Browses extensions offered in installed `ClusterCatalogs`
- Derives minimum privilege for installation
- filters visibility on installable extensions
- Verifies that extension health is detectable to desired sensors

## Cluster Catalog Admin
*Who is it?*

This role encompasses the control of `ClusterCatalogs` on the running cluster.  This role may exist as a set of activities executed by a cluster admin, but also may operate independently of that role, depending on the necessary privileges.  This role is a collaboration with **Catalog Curators** and may also interact with **Catalog Manipulators**

*What does it do?*

- Adds/removes/updates catalogs
- Enables/disables catalogs
- Configures pull secrets necessary to access extensions from catalogs

## Cluster Monitors
*Who is it?*

This role represents any actor which monitors the status of the cluster and installed workloads.  This may include
- Platform status
- Extension health
- Diagnostic notifications


# Producers
## Extension Author
*Who is it?*

This role encompasses folks who want to create an extension.  It interacts with other **Producer** roles by generating a _catalog contribution_ to make extensions available on-cluster to **Cluster Extension Admins**. For example, a catalog contribution for a registry+v1 bundle is one/more bundle image and the upgrade graph expressed in FBC.

*What does it do?*
- Creates extension
- Builds/releases extension
- Validates extension
- Adjusts upgrade graph
- Publishes artifacts (i.e. images for registry+v1 bundle)

## Contribution Curator
*Who is it?*

This role is responsible for taking catalog contributions from **Extension Authors**, applying any changes necessary for publication, and supplying the resulting artifacts to the **Catalog Curator**. This role is frequently fulfilled by different developers than **Extension Authors**. 

*What does it do?*
- Validates contributions
- Publishes contributions to registry

## Catalog Curator
*Who is it?*

This role is responsible for publishing a catalog index image to be used by **Consumers** to make workloads available on-cluster.  Typically this role operates over multiple extensions, versions, and versioned releases of the final, published catalog. 

*What does it do?*
- Aggregates contributions
- Validates aggregate catalog
- Publishes aggregate catalog

## Catalog Manipulator
*Who is it?*

This role is a general category for users who consume published catalogs and re-publish them in some way.  Possible use-cases include
- Restricting available extension versions
- Providing enclave services to disconnected environments
- Reducing catalog size by restricting the number of included extensions

*What does it do?*
- Filters content
- Defines content access mapping to new environments (if modified)
- Provides catalog access in restricted environments



