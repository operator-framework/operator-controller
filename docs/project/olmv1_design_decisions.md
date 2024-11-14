# Multi-Tenancy Challenges, Lessons Learned, and Design Shifts

This provides historical context on the design explorations and challenges that led to substantial design shifts between
OLM v1 and its predecessor. It explains the technical reasons why OLM v1 cannot support major v0 features, such as,
multi-tenancy, and namespace-specific controller configurations. Finally, it highlights OLM v1’s shift toward
more secure, predictable, and simple operations while moving away from some of the complex, error-prone features of OLM v0.

## What won't OLM v1 do that OLM v0 did?

TL;DR: OLM v1 cannot feasibly support multi-tenancy or any feature that assumes multi-tenancy. All multi-tenancy features end up falling over because of the global API system of Kubernetes. While this short conclusion may be unsatisfying, the reasons are complex and intertwined.

### Historical Context

Nearly every active contributor in the Operator Framework project contributed to design explorations and prototypes over an entire year. For each of these design explorations, there are complex webs of features and assumptions that are necessary to understand the context that ultimately led to a conclusion of infeasibility.

Here is a sampling of some of the ideas we explored:

- [OLM v1's approach to multi-tenancy](https://docs.google.com/document/d/1xTu7XadmqD61imJisjnP9A6k38_fiZQ8ThvZSDYszog/edit#heading=h.m19itc78n5rw)
- [OLM v1 Multi-tenancy Brainstorming](https://docs.google.com/document/d/1ihFuJR9YS_GWW4_p3qjXu3WjvK0NIPIkt0qGixirQO8/edit#heading=h.vy9860qq1j01)

### Watched namespaces cannot be configured in a first-class API

OLM v1 will not have a first-class API for configuring the namespaces that a controller will watch.

Kubernetes APIs are global. Kubernetes is designed with the assumption that a controller WILL reconcile an object no matter where it is in the cluster.

However, Kubernetes does not assume that a controller will be successful when it reconciles an object.

The Kubernetes design assumptions are:

- CRDs and their controllers are trusted cluster extensions.
- If an object for an API exists a controller WILL reconcile it, no matter where it is in the cluster.

OLM v1 will make the same assumption that Kubernetes does and that users of Kubernetes APIs do. That is: If a user has RBAC to create an object in the cluster, they can expect that a controller exists that will reconcile that object. If this assumption does not hold, it will be considered a configuration issue, not an OLM v1 bug.

This means that it is a best practice to implement and configure controllers to have cluster-wide permission to read and update the status of their primary APIs. It does not mean that a controller needs cluster-wide access to read/write secondary APIs. If a controller can update the status of its primary APIs, it can tell users when it lacks permission to act on secondary APIs.

### Dependencies based on watched namespaces

Since there will be no first-class support for configuration of watched namespaces, OLM v1 cannot resolve dependencies among bundles based on where controllers are watching.

However, not all bundle constraints are based on dependencies among bundles from different packages. OLM v1 will be able to support constraints against cluster state. For example, OLM v1 could support a “kubernetesVersionRange” constraint that blocks installation of a bundle if the current kubernetes cluster version does not fall into the specified range.

#### Background

For packages that specify API-based dependencies, OLMv0’s dependency checker knows which controllers are watching which namespaces. While OLM v1 will have awareness of which APIs are present on a cluster (via the discovery API), it will not know which namespaces are being watched for reconciliation of those APIs. Therefore dependency resolution based solely on API availability would only work in cases where controllers are configured to watch all namespaces.

For packages that specify package-based dependencies, OLMv0’s dependency checker again knows which controllers are watching which namespaces. This case is challenging for a variety of reasons:

1. How would a dependency resolver know which extensions were installed (let alone which extensions were watching which namespaces)? If a user is running the resolver, they would be blind to an installed extension that is watching their namespace if they don’t have permission to list extensions in the installation namespace. If a controller is running the resolver, then it might leak information to a user about installed extensions that the user is not otherwise entitled to know.
2. Even if (1) could be overcome, the lack of awareness of watched namespaces means that the resolver would have to make assumptions. If only one controller is installed, is it watching the right set of namespaces to meet the constraint? If multiple controllers are installed, are any of them watching the right set of namespaces? Without knowing the watched namespaces of the parent and child controllers, a correct dependency resolver implementation is not possible to implement.

Note that regardless of the ability of OLM v1 to perform dependency resolution (now or in the future), OLM v1 will not automatically install a missing dependency when a user requests an operator. The primary reasoning is that OLM v1 will err on the side of predictability and cluster-administrator awareness.

### "Watch namespace"-aware operator discoverability

When operators add APIs to a cluster, these APIs are globally visible. As stated before, there is an assumption in this design that a controller will reconcile an object of that API anywhere it exists in the cluster.

Therefore, the API discoverability story boils down to answering this question for the user: “What APIs do I have access to in a given namespace?” Fortunately, built-in APIs exist to answer this question: Kubernetes Discovery, SelfSubjectRulesReview (SSRR), and SelfSubjectAccessReview (SSAR).

However, helping users discover which actual controllers will reconcile those APIs is not possible unless OLM v1 knows which namespaces those controllers are watching.

Any solution here would be unaware of where a controller is actually watching and could only know “is there a controller installed that provides an implementation of this API?”. However even knowledge of a controller installation is not certain. Any user can use the discovery, SSRR, and SSAR. Not all users can list all Extensions (see [User discovery of “available” APIs](#user-discovery-of-available-apis)).

## What does this mean for multi-tenancy?

The multi-tenancy promises that OLMv0 made were false promises. Kubernetes is not multi-tenant with respect to management of APIs (because APIs are global). Any promise that OLMv0 has around multi-tenancy evaporates when true tenant isolation attempts are made, and any attempt to fix a broken promise is actually just a bandaid on an already broken assumption.

So where do we go from here? There are multiple solutions that do not involve OLM implementing full multi-tenancy support, some or all of which can be explored.

1. Customers transition to a control plane per tenant
2. Extension authors update their operators to support customers’ multi-tenancy use cases
3. Extension authors with “simple” lifecycling concerns transition to other packaging and deployment strategies (e.g. helm charts)

### Single-tenant control planes

One choice for customers would be to adopt low-overhead single-tenant control planes in which every tenant can have full control over their APIs and controllers and be truly isolated (at the control plane layer at least) from other tenants. With this option, the things OLM v1 cannot do (listed above) are irrelevant, because the purpose of all of those features is to support multi-tenant control planes in OLM.

The [Kubernetes multi-tenancy docs](https://kubernetes.io/docs/concepts/security/multi-tenancy/#virtual-control-plane-per-tenant) contain a good overview of the options in this space. Kubernetes vendors may also have their own virtual control plane implementations.

### Shift multi-tenant responsibility to operators

There is a set of operators that both (a) provide fully namespace-scoped workload-style operands and that (b) provide a large amount of value to their users for advanced features like backup and migration. For these operators, the Operator Framework program would suggest that they shift toward supporting multi-tenancy directly. That would involve:

1. Taking extreme care to avoid API breaking changes.
2. Supporting multiple versions of their operands in a single version of the operator (if required by users in multi-tenant clusters).
3. Maintaining support for versioned operands for the same period of time that the operator is supported for a given cluster version.
4. Completely avoiding global configuration. Each tenant should be able to provide their configuration separately.

### Operator authors ship controllers outside of OLM

Some projects have been successful delivering and supporting their operator on Kubernetes, but outside of OLM, for example with helm-packaged operators. On this path, individual layered project teams have more flexibility in solving lifecycling problems for their users because they are unencumbered by OLM’s opinions. However the tradeoff is that those project teams and their users take on responsibility and accountability for safe upgrades, automation, and multi-tenant architectures. With OLM v1 no longer attempting to support multi-tenancy in a first-class way, these tradeoffs change and project teams may decide that a different approach is necessary.

This path does not necessarily mean a scattering of content in various places. It would still be possible to provide customers with a marketplace of content (e.g. see [artifacthub.io](https://artifacthub.io/)).

### Authors of "simple" operators ship their workload without an operator

Another direction is to just stop shipping content via an operator. The operator pattern is particularly well-suited for applications that require complex upgrade logic, that need to convert declarative intent to imperative action, or that require sophisticated health monitoring logic and feedback. But a sizable portion of the OperatorHub catalog contain operators that are not actually taking advantage of the benefits of the operator pattern and are instead a simple wrapper around the workload, which is where the real value is.

Using the [Operator Capability Levels](https://sdk.operatorframework.io/docs/overview/operator-capabilities/) as a rubric, operators that fall into Level 1 and some that fall into Level 2 are not making full use of the operator pattern. If content authors had the choice to ship their content without also shipping an operator that performs simple installation and upgrades, many supporting these Level 1 and Level 2 operators might make that choice to decrease their overall maintenance and support burden while losing very little in terms of value to their customers.

## What will OLM do that a generic package manager doesn't?

OLM will provide multiple features that are absent in generic package managers. Some items listed below are already implemented, while others may be planned for the future.

### Upgrade controls

An operator author can control the upgrade flow by specifying supported upgrade paths from one version to another. Or, they could use semantic versioning (semver) - this is fully supported by OLM too.

A user can see the author’s supplied upgrade information.

While these features might seem standard in package managers, they are fairly unique in the Kubernetes ecosystem. Helm, for example, doesn’t have any features that help users stay on supported upgrade paths.

### On-cluster component for automated upgrades, health monitoring

OLM automatically upgrades an operator to the latest acceptable matching version whenever a new matching version appears in a catalog, assuming the user has enabled this for their operator.

OLM constantly monitors the state of all on-cluster resources for all the operators it manages, reporting the health in aggregate on each operator.

### CRD Upgrade Safety Checks

Before OLM upgrades a CRD, OLM performs a set of safety checks to identify any changes that potentially would have negative impacts, such as:

- data loss
- incompatible schema changes

These checks may not be a guarantee that an upgrade is safe; instead, they are intended to provide an early warning sign for identifiable incompatibilities. False positives (OLM v1 claims a breaking change when there is none) and false negatives (a breaking change makes it through the check without being caught) are possible, at least while the OLM v1 team iterates on this feature.

### User permissions management

Operators typically provide at least one new API, and often multiple. While operator authors know the APIs they’re providing, users installing operators won’t necessarily have this same knowledge. OLM will make it easy to grant permissions to operator-provided APIs to users/groups in various namespaces, but any automation (which would be client-side only) or UX provided by OLM related to user permissions management will be unable to automatically account for watch namespace configurations. See [Watched namespaces cannot be configured in a first class API](#watched-namespaces-cannot-be-configured-in-a-first-class-api)

Also note that user permission management does not unlock operator discoverability (only API discoverability). See [“Watch namespace”-aware operator discoverability](#watch-namespace-aware-operator-discoverability) for more details.

### User discovery of “available” APIs

In the future, the Operator Framework team could explore building an API similar to SelfSubjectAccessReview and SelfSubjectRulesReview that answers the question:
“What is the public metadata of all extensions that are installed on the cluster that provide APIs that I have permission for in namespace X?”

One solution would be to join “installed extensions with user permissions”. If an installed extension provides an API that a user has RBAC permission for, that extension would be considered available to that user in that scope. This solution would not be foolproof: it makes the (reasonable) assumption that an administrator only configures RBAC for a user in a namespace where a controller is reconciling that object. If an administrator gives a user RBAC access to an API without also configuring that controller to watch the namespace that they have access to, the discovery solution would report an available extension, but then nothing would actually reconcile the object they create.

This solution would tell users about API-only and API+controller bundles that are installed. It would not tell users about controller-only bundles, because they do not include APIs.

Other similar API-centric solutions could be explored as well. For example, pursuing enhancements to OLM v1 or core Kubernetes related to API metadata and/or grouping.

A key insight here is that controller-specific metadata like the version of the controller that will reconcile the object in a certain namespace is not necessary for discovery. Discovery is primarily about driving user flows around presenting information and example usage of a group of APIs such that CLIs and UIs can provide rich experiences around interactions with available APIs.

## Approach

We will adhere to the following tenets in our approach for the design and implementation of OLM v1

### Do not fight Kubernetes

One of the key features of cloud-native applications/extensions/operators is that they typically come with a Kubernetes-based API (e.g. CRD) and a controller that reconciles instances of that API. In Kubernetes, API registration is cluster-scoped. It is not possible to register different APIs in different namespaces. Instances of an API can be cluster- or namespace-scoped. All APIs are global (they can be invoked/accessed regardless of namespace). For cluster-scoped APIs, the names of their instances must be unique. For example, it’s possible to have Nodes named “one” and “two”, but it’s not possible to have multiple Nodes named “two”. For namespace-scoped APIs, the names of their instances must be unique per namespace. The following illustrates this for ConfigMaps, a namespace-scoped API:

Allowed

- Namespace: test, name: my-configmap
- Namespace: other, name: my-configmap

Disallowed

- Namespace: test, name: my-configmap
- Namespace: test, name: my-configmap

In cases where OLMv0 decides that joint ownership of CRDs will not impact different tenants, OLMv0 allows multiple installations of bundles that include the same named CRD, and OLMv0 itself manages the CRD lifecycle. This has security implications because it requires OLMv0 to act as a deputy, but it also pits OLM against the limitations of the Kubernetes API. OLMv0 promises that different versions of an operator can be installed in the cluster for use by different tenants without tenants being affected by each other. This is not a promise OLM can make because it is not possible to have multiple versions of the same CRD present on a cluster for different tenants.

In OLM v1, we will not design the core APIs and controllers around this promise. Instead, we will build an API where ownership of installed objects is not shared. Managed objects are owned by exactly one extension.

This pattern is generic, aligns with the Kubernetes API, and makes multi-tenancy a possibility, but not a guarantee or core concept. We will explore the implications of this design on existing OLMv0 registry+v1 bundles as part of the larger v0 to v1 migration design. For net new content, operator authors that intend multiple installations of operator on the same cluster would need to package their components to account for this ownership rule. Generally, this would entail separation along these lines:

- CRDs, conversion webhook workloads, and admission webhook configurations and workloads, APIServices and workloads.
- Controller workloads, service accounts, RBAC, etc.

OLM v1 will include primitives (e.g. templating) to make it possible to have multiple non-conflicting installations of bundles.

However, it should be noted that the purpose of these primitives is not to enable multi-tenancy. It is to enable administrators to provide configuration for the installation of an extension. The fact that operators can be packaged as separate bundles and parameterized in a way that permits multiple controller installations is incidental, and not something that OLM v1 will encourage or promote.

### Make OLM secure by default

OLMv0 runs as cluster-admin, which is a security concern. OLMv0 has optional security controls for operator installations via the OperatorGroup, which allows a user with permission to create or update them to also set a ServiceAccount that will be used for authorization purposes on operator installations and upgrades in that namespace. If a ServiceAccount is not explicitly specified, OLM’s cluster-admin credentials are used. Another avenue that cluster administrators have is to lock down permissions and usage of the CatalogSource API, disable default catalogs, and provide tenants with custom vetted catalogs. However if a cluster admin is not aware of these options, the default configuration of a cluster results in users with permission to create a Subscription in namespaces that contain an OperatorGroup effectively have cluster-admin, because OLMv0 has unlimited permissions to install any bundle available in the default catalogs and the default community catalog is not vetted for limited RBAC. Because OLMv0 is used to install more RBAC and run arbitrary workloads, there are numerous potential vectors that attackers could exploit. While there are no known exploits and there has not been any specific concern reported from customers, we believe CNCF’s reputation rest on secure cloud-native software and that this is a non-negotiable area to improve.

To make OLM secure by default:

- OLM v1 will not be granted cluster admin permissions. Instead, it will require service accounts provided by users to actually install, upgrade, and delete content. In addition to the security this provides, it also fulfills one of OLM’s long-standing requirements: halt when bundle upgrades require additional permissions and wait until those permissions are granted.
- OLM v1 will use secure communication protocols between all internal components and between itself and its clients.

### Simple and predictable semantics for install, upgrade, and delete

OLMv0 has grown into a complex web of functionality that is difficult to understand, even for seasoned Kubernetes veterans.

In OLM v1 we will move to GitOps-friendly APIs that allow administrators to rely on their experience with conventional Kubernetes API behavior (declarative, eventually consistent) to manage operator lifecycles.

OLM v1 will reduce its API surface down to two primary APIs that represent catalogs of content, and intent for that content to be installed on the cluster.

OLM v1 will:

- Permit administrators to pin to specific versions, channels, version ranges, or combinations of both.
- Permit administrators to pause management of an installation for maintenance or troubleshooting purposes.
- Put opinionated guardrails up by default (e.g. follow operator developer-defined upgrade edges).
- Give administrators escape hatches to disable any or all of OLMs guardrails.
- Delete managed content when a user deletes the OLM object that represents it.

### APIs and behaviors to handle common controller patterns

OLMv0 takes an extremely opinionated stance on the contents of the bundles it installs and in the way that operators can be lifecycled. The original designers believed these opinions would keep OLM’s scope limited and that they encompassed best practices for operator lifecycling. Some of these opinions are:

- All bundles must include a ClusterServiceVersion, which ostensibly gives operator authors an API that they can use to fully describe how to run the operator, what permissions it requires, what APIs it provides, and what metadata to show to users.
- Bundles cannot contain arbitrary objects. OLMv0 needs to have specific handling for each resource that it supports.
- Cluster administrators cannot override OLM safety checks around CRD changes or upgrades.

OLM v1 will take a slightly different approach:

- It will not require bundles to have very specific controller-centric shapes. OLM v1 will happily install a bundle that contains a deployment, service, and ingress or a bundle that contains a single configmap.
- However for bundles that do include CRDs, controllers, RBAC, webhooks, and other objects that relate to the behavior of the apiserver, OLM will continue to have opinions and special handling:
    - CRD upgrade checks (best effort)
    - Specific knowledge and handling of webhooks.
- To the extent necessary OLM v1 will include optional controller-centric concepts in its APIs and or CLIs in order to facilitate the most common controller patterns. Examples could include:
    - Permission management
    - CRD upgrade check policies
- OLM v1 will continue to have opinions about upgrade traversals and CRD changes that help users prevent accidental breakage, but it will also allow administrators to disable safeguards and proceed anyway.

OLMv0 has some support for automatic upgrades. However, administrators cannot control the maximum version for automatic upgrades, and the upgrade policy (manual vs automatic) applies to all operators in a namespace. If any operator’s upgrade policy is manual, all upgrades of all operators in the namespace must be approved manually.

OLM v1 will have fine-grained control for version ranges (and pins) and for controlling automatic upgrades for individual operators regardless of the policy of other operators installed in the same namespace.

### Constraint checking (but not automated on-cluster management)

OLMv0 includes support for dependency and constraint checking for many common use cases (e.g. required and provided APIs, required cluster version, required package versions). It also has other constraint APIs that have not gained traction (e.g. CEL expressions and compound constraints). In addition to its somewhat hap-hazard constraint expression support, OLMv0 also automatically installs dependency trees, which has proven problematic in several respects:

1. OLMv0 can resolve existing dependencies from outside the current namespace, but it can only install new dependencies in the current namespace. One scenario where this is problematic is if A depends on B, where A supports only OwnNamespace mode and B supports only AllNamespace mode. In that case, OLMv0’s auto dependency management fails because B cannot be installed in the same namespace as A (assuming the OperatorGroup in that namespace is configured for OwnNamespace operators to work).
2. OLMv0’s logic for choosing a dependency among multiple contenders is confusing and error-prone, and an administrator’s ability to have fine-grained control of upgrades is essentially limited to building and deploying tailor-made catalogs.
3. OLMv0 automatically installs dependencies. The only way for an administrator to avoid this OLMv0 functionality is to fully understand the dependency tree in advance and to then install dependencies from the leaves to the root so that OLMv0 always detects that dependencies are already met. If OLMv0 installs a dependency for you, it does not uninstall it when it is no longer depended upon.

OLM v1 will not provide dependency resolution among packages in the catalog (see [Dependencies based on watched namespaces](#dependencies-based-on-watched-namespaces))

OLM v1 will provide constraint checking based on available cluster state. Constraint checking will be limited to checking whether the existing constraints are met. If so, install proceeds. If not, unmet constraints will be reported and the install/upgrade waits until constraints are met.

The Operator Framework team will perform a survey of registry+v1 packages that currently rely on OLMv0’s dependency features and will suggest a solution as part of the overall OLMv0 to OLM v1 migration effort.

### Client libraries and CLIs contribute to the overall UX

OLMv0 has no official client libraries or CLIs that can be used to augment its functionality or provide a more streamlined user experience. The kubectl "operator" plugin was developed several years ago, but has never been a focus of the core Operator Framework development team, and has never factored into the overall architecture.

OLM v1 will deliver an official CLI (likely by overhauling the kubectl operator plugin) and will use it to meet requirements that are difficult or impossible to implement in a controller, or where an architectural assessment dictates that a client is the better choice. This CLI would automate standard workflows over cluster APIs to facilitate simple administrative actions (e.g. automatically create RBAC and ServiceAccounts necessary for an extension installation as an optional step in the CLI’s extension install experience).

The official CLI will provide administrators and users with a UX that covers the most common scenarios users will encounter.

The official CLI will explicitly NOT attempt to cover complex scenarios. Maintainers will reject requests to over-complicate the CLI. Users with advanced use cases will be able to directly interact with OLM v1’s on-cluster APIs.

The idea is:

- On-cluster APIs can be used to manage operators in 100% of cases (assuming bundle content is structured in a compatible way)
- The official CLI will cover standard user flows, covering ~80% of use cases.
- Third-party or unofficial CLIs will cover the remaining ~20% of use cases.

Areas where the official CLI could provide value include:

- Catalog interactions (search, list, inspect, etc.)
- Standard install/upgrade/delete commands
- Upgrade previews
- RBAC management
- Discovery of available APIs
