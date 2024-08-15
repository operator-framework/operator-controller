OLM v1 is composed of various component projects: 

* [operator-controller](https://github.com/operator-framework/operator-controller): operator-controller is the central component of OLM v1, that consumes all of the components below to extend Kubernetes to allows users to install, and manage the lifecycle of other extensions

* [catalogD](https://github.com/operator-framework/catalogd): Catalogd is a Kubernetes extension that unpacks [file-based catalog (FBC)](https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs) content that is packaged and shipped in container images, for consumption by clients on-clusters (unpacking from other sources, like git repos, OCI artifacts etc, are in the roadmap for catalogD). As component of the Operator Lifecycle Manager (OLM) v1 microservices architecture, catalogD hosts metadata for Kubernetes extensions packaged by the authors of the extensions, as a result helping customers discover installable content.
