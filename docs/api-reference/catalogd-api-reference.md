# API Reference

## Packages
- [olm.operatorframework.io/v1](#olmoperatorframeworkiov1)


## olm.operatorframework.io/v1

Package v1 contains API Schema definitions for the core v1 API group

### Resource Types
- [ClusterCatalog](#clustercatalog)
- [ClusterCatalogList](#clustercataloglist)



#### AvailabilityMode

_Underlying type:_ _string_

AvailabilityMode defines the availability of the catalog



_Appears in:_
- [ClusterCatalogSpec](#clustercatalogspec)

| Field | Description |
| --- | --- |
| `Available` |  |
| `Unavailable` |  |


#### CatalogSource



CatalogSource is a discriminated union of possible sources for a Catalog.
CatalogSource contains the sourcing information for a Catalog



_Appears in:_
- [ClusterCatalogSpec](#clustercatalogspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[SourceType](#sourcetype)_ | type is a reference to the type of source the catalog is sourced from.<br />type is required.<br /><br />The only allowed value is "Image".<br /><br />When set to "Image", the ClusterCatalog content will be sourced from an OCI image.<br />When using an image source, the image field must be set and must be the only field defined for this type. |  | Enum: [Image] <br />Required: \{\} <br /> |
| `image` _[ImageSource](#imagesource)_ | image is used to configure how catalog contents are sourced from an OCI image.<br />This field is required when type is Image, and forbidden otherwise. |  |  |


#### ClusterCatalog



ClusterCatalog enables users to make File-Based Catalog (FBC) catalog data available to the cluster.
For more information on FBC, see https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs



_Appears in:_
- [ClusterCatalogList](#clustercataloglist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `olm.operatorframework.io/v1` | | |
| `kind` _string_ | `ClusterCatalog` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ClusterCatalogSpec](#clustercatalogspec)_ | spec is the desired state of the ClusterCatalog.<br />spec is required.<br />The controller will work to ensure that the desired<br />catalog is unpacked and served over the catalog content HTTP server. |  | Required: \{\} <br /> |
| `status` _[ClusterCatalogStatus](#clustercatalogstatus)_ | status contains information about the state of the ClusterCatalog such as:<br />  - Whether or not the catalog contents are being served via the catalog content HTTP server<br />  - Whether or not the ClusterCatalog is progressing to a new state<br />  - A reference to the source from which the catalog contents were retrieved |  |  |


#### ClusterCatalogList



ClusterCatalogList contains a list of ClusterCatalog





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `olm.operatorframework.io/v1` | | |
| `kind` _string_ | `ClusterCatalogList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ClusterCatalog](#clustercatalog) array_ | items is a list of ClusterCatalogs.<br />items is required. |  | Required: \{\} <br /> |


#### ClusterCatalogSpec



ClusterCatalogSpec defines the desired state of ClusterCatalog



_Appears in:_
- [ClusterCatalog](#clustercatalog)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `source` _[CatalogSource](#catalogsource)_ | source allows a user to define the source of a catalog.<br />A "catalog" contains information on content that can be installed on a cluster.<br />Providing a catalog source makes the contents of the catalog discoverable and usable by<br />other on-cluster components.<br />These on-cluster components may do a variety of things with this information, such as<br />presenting the content in a GUI dashboard or installing content from the catalog on the cluster.<br />The catalog source must contain catalog metadata in the File-Based Catalog (FBC) format.<br />For more information on FBC, see https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs.<br />source is a required field.<br /><br />Below is a minimal example of a ClusterCatalogSpec that sources a catalog from an image:<br /><br /> source:<br />   type: Image<br />   image:<br />     ref: quay.io/operatorhubio/catalog:latest |  | Required: \{\} <br /> |
| `priority` _integer_ | priority allows the user to define a priority for a ClusterCatalog.<br />priority is optional.<br /><br />A ClusterCatalog's priority is used by clients as a tie-breaker between ClusterCatalogs that meet the client's requirements.<br />A higher number means higher priority.<br /><br />It is up to clients to decide how to handle scenarios where multiple ClusterCatalogs with the same priority meet their requirements.<br />When deciding how to break the tie in this scenario, it is recommended that clients prompt their users for additional input.<br /><br />When omitted, the default priority is 0 because that is the zero value of integers.<br /><br />Negative numbers can be used to specify a priority lower than the default.<br />Positive numbers can be used to specify a priority higher than the default.<br /><br />The lowest possible value is -2147483648.<br />The highest possible value is 2147483647. | 0 |  |
| `availabilityMode` _[AvailabilityMode](#availabilitymode)_ | availabilityMode allows users to define how the ClusterCatalog is made available to clients on the cluster.<br />availabilityMode is optional.<br /><br />Allowed values are "Available" and "Unavailable" and omitted.<br /><br />When omitted, the default value is "Available".<br /><br />When set to "Available", the catalog contents will be unpacked and served over the catalog content HTTP server.<br />Setting the availabilityMode to "Available" tells clients that they should consider this ClusterCatalog<br />and its contents as usable.<br /><br />When set to "Unavailable", the catalog contents will no longer be served over the catalog content HTTP server.<br />When set to this availabilityMode it should be interpreted the same as the ClusterCatalog not existing.<br />Setting the availabilityMode to "Unavailable" can be useful in scenarios where a user may not want<br />to delete the ClusterCatalog all together, but would still like it to be treated as if it doesn't exist. | Available | Enum: [Unavailable Available] <br /> |


#### ClusterCatalogStatus



ClusterCatalogStatus defines the observed state of ClusterCatalog



_Appears in:_
- [ClusterCatalog](#clustercatalog)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | conditions is a representation of the current state for this ClusterCatalog.<br /><br />The current condition types are Serving and Progressing.<br /><br />The Serving condition is used to represent whether or not the contents of the catalog is being served via the HTTP(S) web server.<br />When it has a status of True and a reason of Available, the contents of the catalog are being served.<br />When it has a status of False and a reason of Unavailable, the contents of the catalog are not being served because the contents are not yet available.<br />When it has a status of False and a reason of UserSpecifiedUnavailable, the contents of the catalog are not being served because the catalog has been intentionally marked as unavailable.<br /><br />The Progressing condition is used to represent whether or not the ClusterCatalog is progressing or is ready to progress towards a new state.<br />When it has a status of True and a reason of Retrying, there was an error in the progression of the ClusterCatalog that may be resolved on subsequent reconciliation attempts.<br />When it has a status of True and a reason of Succeeded, the ClusterCatalog has successfully progressed to a new state and is ready to continue progressing.<br />When it has a status of False and a reason of Blocked, there was an error in the progression of the ClusterCatalog that requires manual intervention for recovery.<br /><br />In the case that the Serving condition is True with reason Available and Progressing is True with reason Retrying, the previously fetched<br />catalog contents are still being served via the HTTP(S) web server while we are progressing towards serving a new version of the catalog<br />contents. This could occur when we've initially fetched the latest contents from the source for this catalog and when polling for changes<br />to the contents we identify that there are updates to the contents. |  |  |
| `resolvedSource` _[ResolvedCatalogSource](#resolvedcatalogsource)_ | resolvedSource contains information about the resolved source based on the source type. |  |  |
| `urls` _[ClusterCatalogURLs](#clustercatalogurls)_ | urls contains the URLs that can be used to access the catalog. |  |  |
| `lastUnpacked` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#time-v1-meta)_ | lastUnpacked represents the last time the contents of the<br />catalog were extracted from their source format. As an example,<br />when using an Image source, the OCI image will be pulled and the<br />image layers written to a file-system backed cache. We refer to the<br />act of this extraction from the source format as "unpacking". |  |  |


#### ClusterCatalogURLs



ClusterCatalogURLs contains the URLs that can be used to access the catalog.



_Appears in:_
- [ClusterCatalogStatus](#clustercatalogstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `base` _string_ | base is a cluster-internal URL that provides endpoints for<br />accessing the content of the catalog.<br /><br />It is expected that clients append the path for the endpoint they wish<br />to access.<br /><br />Currently, only a single endpoint is served and is accessible at the path<br />/api/v1.<br /><br />The endpoints served for the v1 API are:<br />  - /all - this endpoint returns the entirety of the catalog contents in the FBC format<br /><br />As the needs of users and clients of the evolve, new endpoints may be added. |  | MaxLength: 525 <br />Required: \{\} <br /> |


#### ImageSource



ImageSource enables users to define the information required for sourcing a Catalog from an OCI image


If we see that there is a possibly valid digest-based image reference AND pollIntervalMinutes is specified,
reject the resource since there is no use in polling a digest-based image reference.



_Appears in:_
- [CatalogSource](#catalogsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ref` _string_ | ref allows users to define the reference to a container image containing Catalog contents.<br />ref is required.<br />ref can not be more than 1000 characters.<br /><br />A reference can be broken down into 3 parts - the domain, name, and identifier.<br /><br />The domain is typically the registry where an image is located.<br />It must be alphanumeric characters (lowercase and uppercase) separated by the "." character.<br />Hyphenation is allowed, but the domain must start and end with alphanumeric characters.<br />Specifying a port to use is also allowed by adding the ":" character followed by numeric values.<br />The port must be the last value in the domain.<br />Some examples of valid domain values are "registry.mydomain.io", "quay.io", "my-registry.io:8080".<br /><br />The name is typically the repository in the registry where an image is located.<br />It must contain lowercase alphanumeric characters separated only by the ".", "_", "__", "-" characters.<br />Multiple names can be concatenated with the "/" character.<br />The domain and name are combined using the "/" character.<br />Some examples of valid name values are "operatorhubio/catalog", "catalog", "my-catalog.prod".<br />An example of the domain and name parts of a reference being combined is "quay.io/operatorhubio/catalog".<br /><br />The identifier is typically the tag or digest for an image reference and is present at the end of the reference.<br />It starts with a separator character used to distinguish the end of the name and beginning of the identifier.<br />For a digest-based reference, the "@" character is the separator.<br />For a tag-based reference, the ":" character is the separator.<br />An identifier is required in the reference.<br /><br />Digest-based references must contain an algorithm reference immediately after the "@" separator.<br />The algorithm reference must be followed by the ":" character and an encoded string.<br />The algorithm must start with an uppercase or lowercase alpha character followed by alphanumeric characters and may contain the "-", "_", "+", and "." characters.<br />Some examples of valid algorithm values are "sha256", "sha256+b64u", "multihash+base58".<br />The encoded string following the algorithm must be hex digits (a-f, A-F, 0-9) and must be a minimum of 32 characters.<br /><br />Tag-based references must begin with a word character (alphanumeric + "_") followed by word characters or ".", and "-" characters.<br />The tag must not be longer than 127 characters.<br /><br />An example of a valid digest-based image reference is "quay.io/operatorhubio/catalog@sha256:200d4ddb2a73594b91358fe6397424e975205bfbe44614f5846033cad64b3f05"<br />An example of a valid tag-based image reference is "quay.io/operatorhubio/catalog:latest" |  | MaxLength: 1000 <br />Required: \{\} <br /> |
| `pollIntervalMinutes` _integer_ | pollIntervalMinutes allows the user to set the interval, in minutes, at which the image source should be polled for new content.<br />pollIntervalMinutes is optional.<br />pollIntervalMinutes can not be specified when ref is a digest-based reference.<br /><br />When omitted, the image will not be polled for new content. |  | Minimum: 1 <br /> |


#### ResolvedCatalogSource



ResolvedCatalogSource is a discriminated union of resolution information for a Catalog.
ResolvedCatalogSource contains the information about a sourced Catalog



_Appears in:_
- [ClusterCatalogStatus](#clustercatalogstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[SourceType](#sourcetype)_ | type is a reference to the type of source the catalog is sourced from.<br />type is required.<br /><br />The only allowed value is "Image".<br /><br />When set to "Image", information about the resolved image source will be set in the 'image' field. |  | Enum: [Image] <br />Required: \{\} <br /> |
| `image` _[ResolvedImageSource](#resolvedimagesource)_ | image is a field containing resolution information for a catalog sourced from an image.<br />This field must be set when type is Image, and forbidden otherwise. |  |  |


#### ResolvedImageSource



ResolvedImageSource provides information about the resolved source of a Catalog sourced from an image.



_Appears in:_
- [ResolvedCatalogSource](#resolvedcatalogsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ref` _string_ | ref contains the resolved image digest-based reference.<br />The digest format is used so users can use other tooling to fetch the exact<br />OCI manifests that were used to extract the catalog contents. |  | MaxLength: 1000 <br />Required: \{\} <br /> |


#### SourceType

_Underlying type:_ _string_

SourceType defines the type of source used for catalogs.



_Appears in:_
- [CatalogSource](#catalogsource)
- [ResolvedCatalogSource](#resolvedcatalogsource)

| Field | Description |
| --- | --- |
| `Image` |  |


