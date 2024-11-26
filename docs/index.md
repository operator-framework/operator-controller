---
hide:
  - toc
---

# Operator Lifecycle Manager

The Operator Lifecycle Manager (OLM) is an open-source project under the [Cloud Native Computing Foundation (CNCF)](https://www.cncf.io/), designed to simplify and centralize the management of Kubernetes cluster extensions. OLM streamlines the process of installing, running, and updating these extensions, making it easier, safer, and more reproducible for cluster and platform administrators alike.

Originally, OLM was focused on managing a specific type of extension known as [Operators](https://operatorhub.io/what-is-an-operator#:~:text=is%20an%20Operator-,What%20is%20an%20Operator%20after%20all%3F,or%20automation%20software%20like%20Ansible.), which are powerful tools that automate the management of complex Kubernetes applications. At its core, an Operator is made up of controllers that automate the lifecycle of applications, paired with:

- One or more Kubernetes API extensions.
- One or more [CustomResourceDefinitions (CRDs)](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/), allowing administrators to define custom resources.

The purpose of OLM is to manage the lifecycle of these extensions—from their packaging and distribution to installation, updates, and eventual removal—helping administrators ensure stability and security across their clusters.

In its first release (OLM v0), the project introduced several important concepts and features aimed at improving the lifecycle management of Kubernetes applications:

- **Dependency Model**: Enables extensions to focus on their primary function by delegating non-essential tasks to other dependencies.
- **Constraint Model**: Allows developers to define compatibility constraints such as conflicting extensions or minimum required Kubernetes versions.
- **Namespace-Based Multi-Tenancy**: Provides a multi-tenancy model to manage multiple extensions without the need for namespace-scoped CRDs.
- **Packaging Model**: Distributes extensions through catalogs, allowing users to browse and install extensions, often with access to the full version history.

Thanks to these innovations, OLM has played a significant role in popularizing Operators throughout the Kubernetes ecosystem. A prime example of its impact is [OperatorHub.io](https://operatorhub.io/), a widely-used platform with over 300 Operators from various vendors, providing users with a central location to discover and install Operators.

## Why Build OLM v1?

After five years of real-world use, OLM has become an essential part of managing Kubernetes Operators. However, over time, the community has gathered valuable insights, uncovering both the strengths and limitations of OLM v0. These findings have led to a comprehensive redesign and the creation of OLM v1, with several key improvements over the initial version:

- **Simpler API and Mental Model**: Streamlined APIs and a more intuitive design, making it easier to understand and work with.
- **Greater Flexibility**: Less rigid automation, allowing for more customization and broader use cases.
- **Beyond Operators**: Support for a wider range of Kubernetes applications, not limited to Operators.
- **Security by Default**: Enhanced security features out-of-the-box, reducing vulnerabilities.
- **Helm Chart and GitOps Support**: Expanded support for popular Kubernetes tools like Helm and GitOps, broadening the range of integration options.

For more details on the evolution of OLM and the roadmap for v1, explore the following resources:

- [Multi-Tenancy Challenges, Lessons Learned, and Design Shifts](project/olmv1_design_decisions.md)
- [OLM v1 Roadmap](project/olmv1_roadmap.md)

## Can I Migrate from OLMv0 to OLMv1?

There is currently no concrete migration strategy due to the [conceptual differences between OLMv0 and OLMv1](project/olmv1_design_decisions.md).
OLMv1, as of writing, supports a subset of the existing content supported by OLMv0.
For more information regarding the current limitations of OLMv1, see [limitations](project/olmv1_limitations.md).

If your current usage of OLMv0 is compatible with the limitations and expectations of OLMv1, you may be able to manually
transition to using OLMv1 following the standard workflows we have documented.
