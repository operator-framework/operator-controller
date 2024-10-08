---
hide:
  - toc
---

# Overview

Operator Lifecycle Manager (OLM) is an open-source [CNCF](https://www.cncf.io/) project with the mission to manage the
lifecycle of cluster extensions centrally and declaratively on Kubernetes clusters. Its purpose is to make installing,
running, and updating functional extensions to the cluster easy, safe, and reproducible for cluster administrators and PaaS administrators.

Previously, OLM was focused on a particular type of cluster extension: [Operators](https://operatorhub.io/what-is-an-operator#:~:text=is%20an%20Operator-,What%20is%20an%20Operator%20after%20all%3F,or%20automation%20software%20like%20Ansible.).
Operators are a method of packaging, deploying, and managing a Kubernetes application. An Operator is composed of one or more controllers paired with one or both of the following objects:

* One or more API extensions
* One or more [CustomResourceDefinitions](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) (CRDs).

OLM helped define lifecycles for these extensions: from packaging and distribution to installation, configuration, upgrade, and removal.

The first iteration of OLM, termed OLM v0, included several concepts and features targeting the stability, security, and supportability of the life-cycled applications, for instance:

* A dependency model that enabled cluster extensions to focus on their primary purpose by delegating out of scope behavior to dependencies
* A constraint model that allowed cluster extension developers to define support limitations such as conflicting extensions, and minimum kubernetes versions
* A namespace-based multi-tenancy model in lieu of namespace-scoped CRDs
* A packaging model in which catalogs of extensions, usually containing the entire version history of each extension, are made available to clusters for cluster users to browse and select from

Since its initial release, OLM has helped catalyse the growth of Operators throughout the Kubernetes ecosystem. [OperatorHub.io](https://operatorhub.io/)
is a popular destination for discovering Operators, and boasts over 300 packages from many different vendors.

## Why are we building OLM v1?

The Operator Lifecycle Manager (OLM) has been in production for over five years, serving as a critical component in managing Kubernetes Operators.
Over this time, the community has gathered valuable insights from real-world usage, identifying both the strengths and limitations of the initial design,
and validating the design's initial assumptions. This process led to a complete redesign and rewrite of OLM that, compared to its predecessor, aims to
provide:

* A simpler API surface and mental model
* Less opinionated automation and greater flexibility
* Support for Kubernetes applications beyond only Operators
* Security by default
* Helm Chart support
* GitOps support

To learn more about where v1 one came from, and where it's going, please see [Multi-Tenancy Challenges, Lessons Learned, and Design Shifts](project/olmv1_design_decisions.md)
and our feature [Roadmap](project/olmv1_roadmap.md).
