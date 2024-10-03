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



CatalogSource is a discriminated union of possible sources for a Catalog.
CatalogSource contains the sourcing information for a Catalog



_Appears in:_
- [ClusterCatalogSpec](#clustercatalogspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[SourceType](#sourcetype)_ | type is a required reference to the type of source the catalog is sourced from.<br /><br />Allowed values are ["Image"]<br /><br />When this field is set to "Image", the ClusterCatalog content will be sourced from an OCI image.<br />When using an image source, the image field must be set and must be the only field defined for this type. |  | Enum: [Image] <br />Required: \{\} <br /> |
| `image` _[ImageSource](#imagesource)_ | image is used to configure how catalog contents are sourced from an OCI image. This field must be set when type is set to "Image" and must be the only field defined for this type. |  |  |


#### ClusterCatalog



ClusterCatalog enables users to make File-Based Catalog (FBC) catalog data available to the cluster.
For more information on FBC, see https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs



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
| `source` _[CatalogSource](#catalogsource)_ | source is a required field that allows the user to define the source of a Catalog that contains catalog metadata in the File-Based Catalog (FBC) format.<br /><br />Below is a minimal example of a ClusterCatalogSpec that sources a catalog from an image:<br /><br /> source:<br />   type: Image<br />   image:<br />     ref: quay.io/operatorhubio/catalog:latest<br /><br />For more information on FBC, see https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs |  |  |
| `priority` _integer_ | priority is an optional field that allows the user to define a priority for a ClusterCatalog.<br />A ClusterCatalog's priority is used by clients as a tie-breaker between ClusterCatalogs that meet the client's requirements.<br />For example, in the case where multiple ClusterCatalogs provide the same bundle.<br />A higher number means higher priority. Negative number as also accepted.<br />When omitted, the default priority is 0. | 0 |  |


#### ClusterCatalogStatus



ClusterCatalogStatus defines the observed state of ClusterCatalog



_Appears in:_
- [ClusterCatalog](#clustercatalog)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | conditions is a representation of the current state for this ClusterCatalog.<br />The status is represented by a set of "conditions".<br /><br />Each condition is generally structured in the following format:<br />  - Type: a string representation of the condition type. More or less the condition "name".<br />  - Status: a string representation of the state of the condition. Can be one of ["True", "False", "Unknown"].<br />  - Reason: a string representation of the reason for the current state of the condition. Typically useful for building automation around particular Type+Reason combinations.<br />  - Message: a human-readable message that further elaborates on the state of the condition.<br /><br />The current set of condition types are:<br />  - "Unpacked", epresents whether, or not, the catalog contents have been successfully unpacked.<br />  - "Deleted", represents whether, or not, the catalog contents have been successfully deleted.<br /><br />The current set of reasons are:<br />  - "UnpackPending", this reason is set on the "Unpack" condition when unpacking the catalog has not started.<br />  - "Unpacking", this reason is set on the "Unpack" condition when the catalog is being unpacked.<br />  - "UnpackSuccessful", this reason is set on the "Unpack" condition when unpacking the catalog is successful and the catalog metadata is available to the cluster.<br />  - "FailedToStore", this reason is set on the "Unpack" condition when an error has been encountered while storing the contents of the catalog.<br />  - "FailedToDelete", this reason is set on the "Delete" condition when an error has been encountered while deleting the contents of the catalog. |  |  |
| `resolvedSource` _[ResolvedCatalogSource](#resolvedcatalogsource)_ | resolvedSource contains information about the resolved source based on the source type.<br /><br />Below is an example of a resolved source for an image source:<br />resolvedSource:<br /><br /> image:<br />   lastPollAttempt: "2024-09-10T12:22:13Z"<br />   lastUnpacked: "2024-09-10T12:22:13Z"<br />   ref: quay.io/operatorhubio/catalog:latest<br />   resolvedRef: quay.io/operatorhubio/catalog@sha256:c7392b4be033da629f9d665fec30f6901de51ce3adebeff0af579f311ee5cf1b<br /> type: Image |  |  |
| `contentURL` _string_ | contentURL is a cluster-internal URL from which on-cluster components<br />can read the content of a catalog |  |  |
| `lastUnpacked` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#time-v1-meta)_ | lastUnpacked represents the time when the<br />ClusterCatalog object was last unpacked successfully. |  |  |


#### ImageSource



ImageSource enables users to define the information required for sourcing a Catalog from an OCI image



_Appears in:_
- [CatalogSource](#catalogsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ref` _string_ | ref is a required field that allows the user to define the reference to a container image containing Catalog contents.<br />Examples:<br />  ref: quay.io/operatorhubio/catalog:latest # image reference<br />  ref: quay.io/operatorhubio/catalog@sha256:c7392b4be033da629f9d665fec30f6901de51ce3adebeff0af579f311ee5cf1b # image reference with sha256 digest |  |  |
| `pollInterval` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#duration-v1-meta)_ | pollInterval is an optional field that allows the user to set the interval at which the image source should be polled for new content.<br />It must be specified as a duration.<br />It must not be specified for a catalog image referenced by a sha256 digest.<br />Examples:<br />  pollInterval: 1h # poll the image source every hour<br />  pollInterval: 30m # poll the image source every 30 minutes<br />  pollInterval: 1h30m # poll the image source every 1 hour and 30 minutes<br /><br />When omitted, the image will not be polled for new content. |  | Format: duration <br /> |


#### ResolvedCatalogSource



ResolvedCatalogSource is a discriminated union of resolution information for a Catalog.
ResolvedCatalogSource contains the information about a sourced Catalog



_Appears in:_
- [ClusterCatalogStatus](#clustercatalogstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[SourceType](#sourcetype)_ | type is a reference to the type of source the catalog is sourced from.<br /><br />It will be set to one of the following values: ["Image"].<br /><br />When this field is set to "Image", information about the resolved image source will be set in the 'image' field. |  | Enum: [Image] <br />Required: \{\} <br /> |
| `image` _[ResolvedImageSource](#resolvedimagesource)_ | image is a field containing resolution information for a catalog sourced from an image. |  |  |


#### ResolvedImageSource



ResolvedImageSource provides information about the resolved source of a Catalog sourced from an image.



_Appears in:_
- [ResolvedCatalogSource](#resolvedcatalogsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ref` _string_ | ref contains the resolved sha256 image ref containing Catalog contents. |  |  |
| `lastSuccessfulPollAttempt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#time-v1-meta)_ | lastSuccessfulPollAttempt is the time when the resolved source was last successfully polled for new content. |  |  |


#### SourceType

_Underlying type:_ _string_

SourceType defines the type of source used for catalogs.



_Appears in:_
- [CatalogSource](#catalogsource)
- [ResolvedCatalogSource](#resolvedcatalogsource)

| Field | Description |
| --- | --- |
| `Image` |  |


