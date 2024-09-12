# API Reference

## Packages
- [olm.operatorframework.io/core](#olmoperatorframeworkiocore)
- [olm.operatorframework.io/v1alpha1](#olmoperatorframeworkiov1alpha1)


## olm.operatorframework.io/core

Package api is the internal version of the API.




## olm.operatorframework.io/v1alpha1

Package v1alpha1 contains API Schema definitions for the core v1alpha1 API group

### Resource Types
- [ClusterCatalog](#clustercatalog)
- [ClusterCatalogList](#clustercataloglist)



#### CatalogSource



CatalogSource contains the sourcing information for a Catalog



_Appears in:_
- [ClusterCatalogSpec](#clustercatalogspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[SourceType](#sourcetype)_ | type defines the kind of Catalog content being sourced. |  | Enum: [image] <br />Required: \{\} <br /> |
| `image` _[ImageSource](#imagesource)_ | image is the catalog image that backs the content of this catalog. |  |  |


#### ClusterCatalog



ClusterCatalog is the Schema for the ClusterCatalogs API



_Appears in:_
- [ClusterCatalogList](#clustercataloglist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `olm.operatorframework.io/v1alpha1` | | |
| `kind` _string_ | `ClusterCatalog` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ClusterCatalogSpec](#clustercatalogspec)_ |  |  |  |
| `status` _[ClusterCatalogStatus](#clustercatalogstatus)_ |  |  |  |


#### ClusterCatalogList



ClusterCatalogList contains a list of ClusterCatalog





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `olm.operatorframework.io/v1alpha1` | | |
| `kind` _string_ | `ClusterCatalogList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ClusterCatalog](#clustercatalog) array_ |  |  |  |


#### ClusterCatalogSpec



ClusterCatalogSpec defines the desired state of ClusterCatalog



_Appears in:_
- [ClusterCatalog](#clustercatalog)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `source` _[CatalogSource](#catalogsource)_ | source is the source of a Catalog that contains catalog metadata in the FBC format<br />https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs |  |  |
| `priority` _integer_ | priority is used as the tie-breaker between bundles selected from different catalogs; a higher number means higher priority. | 0 |  |


#### ClusterCatalogStatus



ClusterCatalogStatus defines the observed state of ClusterCatalog



_Appears in:_
- [ClusterCatalog](#clustercatalog)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | conditions store the status conditions of the ClusterCatalog instances |  |  |
| `resolvedSource` _[ResolvedCatalogSource](#resolvedcatalogsource)_ | resolvedSource contains information about the resolved source |  |  |
| `contentURL` _string_ | contentURL is a cluster-internal address that on-cluster components<br />can read the content of a catalog from |  |  |
| `observedGeneration` _integer_ | observedGeneration is the most recent generation observed for this ClusterCatalog. It corresponds to the<br />ClusterCatalog's generation, which is updated on mutation by the API Server. |  |  |
| `lastUnpacked` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#time-v1-meta)_ | LastUnpacked represents the time when the<br />ClusterCatalog object was last unpacked. |  |  |


#### ImageSource



ImageSource contains information required for sourcing a Catalog from an OCI image



_Appears in:_
- [CatalogSource](#catalogsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ref` _string_ | ref contains the reference to a container image containing Catalog contents. |  |  |
| `pullSecret` _string_ | pullSecret contains the name of the image pull secret in the namespace that catalogd is deployed. |  |  |
| `pollInterval` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#duration-v1-meta)_ | pollInterval indicates the interval at which the image source should be polled for new content,<br />specified as a duration (e.g., "5m", "1h", "24h", "etc".). Note that PollInterval may not be<br />specified for a catalog image referenced by a sha256 digest. |  | Format: duration <br /> |
| `insecureSkipTLSVerify` _boolean_ | insecureSkipTLSVerify indicates that TLS certificate validation should be skipped.<br />If this option is specified, the HTTPS protocol will still be used to<br />fetch the specified image reference.<br />This should not be used in a production environment. |  |  |


#### ResolvedCatalogSource



ResolvedCatalogSource contains the information about a sourced Catalog



_Appears in:_
- [ClusterCatalogStatus](#clustercatalogstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[SourceType](#sourcetype)_ | type defines the kind of Catalog content that was sourced. |  | Enum: [image] <br />Required: \{\} <br /> |
| `image` _[ResolvedImageSource](#resolvedimagesource)_ | image is the catalog image that backs the content of this catalog. |  |  |


#### ResolvedImageSource



ResolvedImageSource contains information about the sourced Catalog



_Appears in:_
- [ResolvedCatalogSource](#resolvedcatalogsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ref` _string_ | ref contains the reference to a container image containing Catalog contents. |  |  |
| `resolvedRef` _string_ | resolvedRef contains the resolved sha256 image ref containing Catalog contents. |  |  |
| `lastPollAttempt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#time-v1-meta)_ | lastPollAtempt is the time when the source resolved was last polled for new content. |  |  |
| `lastUnpacked` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#time-v1-meta)_ | LastUnpacked is the time when the catalog contents were successfully unpacked. |  |  |


#### SourceType

_Underlying type:_ _string_





_Appears in:_
- [CatalogSource](#catalogsource)
- [ResolvedCatalogSource](#resolvedcatalogsource)

| Field | Description |
| --- | --- |
| `image` |  |


