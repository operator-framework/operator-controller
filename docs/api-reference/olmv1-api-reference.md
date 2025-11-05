# API Reference

## Packages
- [olm.operatorframework.io/v1](#olmoperatorframeworkiov1)


## olm.operatorframework.io/v1

Package v1 contains API Schema definitions for the olm v1 API group

### Resource Types
- [ClusterCatalog](#clustercatalog)
- [ClusterCatalogList](#clustercataloglist)
- [ClusterExtension](#clusterextension)
- [ClusterExtensionList](#clusterextensionlist)



#### AvailabilityMode

_Underlying type:_ _string_

AvailabilityMode defines the availability of the catalog



_Appears in:_
- [ClusterCatalogSpec](#clustercatalogspec)

| Field | Description |
| --- | --- |
| `Available` |  |
| `Unavailable` |  |


#### BundleMetadata



BundleMetadata is a representation of the identifying attributes of a bundle.



_Appears in:_
- [ClusterExtensionInstallStatus](#clusterextensioninstallstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is required and follows the DNS subdomain standard<br />as defined in [RFC 1123]. It must contain only lowercase alphanumeric characters,<br />hyphens (-) or periods (.), start and end with an alphanumeric character,<br />and be no longer than 253 characters. |  | Required: \{\} <br /> |
| `version` _string_ | version is a required field and is a reference to the version that this bundle represents<br />version follows the semantic versioning standard as defined in https://semver.org/. |  | Required: \{\} <br /> |


#### CRDUpgradeSafetyEnforcement

_Underlying type:_ _string_





_Appears in:_
- [CRDUpgradeSafetyPreflightConfig](#crdupgradesafetypreflightconfig)

| Field | Description |
| --- | --- |
| `None` | None will not perform CRD upgrade safety checks.<br /> |
| `Strict` | Strict will enforce the CRD upgrade safety check and block the upgrade if the CRD would not pass the check.<br /> |


#### CRDUpgradeSafetyPreflightConfig



CRDUpgradeSafetyPreflightConfig is the configuration for CRD upgrade safety preflight check.



_Appears in:_
- [PreflightConfig](#preflightconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enforcement` _[CRDUpgradeSafetyEnforcement](#crdupgradesafetyenforcement)_ | enforcement is a required field, used to configure the state of the CRD Upgrade Safety pre-flight check.<br />Allowed values are "None" or "Strict". The default value is "Strict".<br />When set to "None", the CRD Upgrade Safety pre-flight check will be skipped<br />when performing an upgrade operation. This should be used with caution as<br />unintended consequences such as data loss can occur.<br />When set to "Strict", the CRD Upgrade Safety pre-flight check will be run when<br />performing an upgrade operation. |  | Enum: [None Strict] <br />Required: \{\} <br /> |


#### CatalogFilter



CatalogFilter defines the attributes used to identify and filter content from a catalog.



_Appears in:_
- [SourceConfig](#sourceconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `packageName` _string_ | packageName is a reference to the name of the package to be installed<br />and is used to filter the content from catalogs.<br />packageName is required, immutable, and follows the DNS subdomain standard<br />as defined in [RFC 1123]. It must contain only lowercase alphanumeric characters,<br />hyphens (-) or periods (.), start and end with an alphanumeric character,<br />and be no longer than 253 characters.<br />Some examples of valid values are:<br />  - some-package<br />  - 123-package<br />  - 1-package-2<br />  - somepackage<br />Some examples of invalid values are:<br />  - -some-package<br />  - some-package-<br />  - thisisareallylongpackagenamethatisgreaterthanthemaximumlength<br />  - some.package<br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxLength: 253 <br />Required: \{\} <br /> |
| `version` _string_ | version is an optional semver constraint (a specific version or range of versions). When unspecified, the latest version available will be installed.<br />Acceptable version ranges are no longer than 64 characters.<br />Version ranges are composed of comma- or space-delimited values and one or<br />more comparison operators, known as comparison strings. Additional<br />comparison strings can be added using the OR operator (\|\|).<br /># Range Comparisons<br />To specify a version range, you can use a comparison string like ">=3.0,<br /><3.6". When specifying a range, automatic updates will occur within that<br />range. The example comparison string means "install any version greater than<br />or equal to 3.0.0 but less than 3.6.0.". It also states intent that if any<br />upgrades are available within the version range after initial installation,<br />those upgrades should be automatically performed.<br /># Pinned Versions<br />To specify an exact version to install you can use a version range that<br />"pins" to a specific version. When pinning to a specific version, no<br />automatic updates will occur. An example of a pinned version range is<br />"0.6.0", which means "only install version 0.6.0 and never<br />upgrade from this version".<br /># Basic Comparison Operators<br />The basic comparison operators and their meanings are:<br />  - "=", equal (not aliased to an operator)<br />  - "!=", not equal<br />  - "<", less than<br />  - ">", greater than<br />  - ">=", greater than OR equal to<br />  - "<=", less than OR equal to<br /># Wildcard Comparisons<br />You can use the "x", "X", and "*" characters as wildcard characters in all<br />comparison operations. Some examples of using the wildcard characters:<br />  - "1.2.x", "1.2.X", and "1.2.*" is equivalent to ">=1.2.0, < 1.3.0"<br />  - ">= 1.2.x", ">= 1.2.X", and ">= 1.2.*" is equivalent to ">= 1.2.0"<br />  - "<= 2.x", "<= 2.X", and "<= 2.*" is equivalent to "< 3"<br />  - "x", "X", and "*" is equivalent to ">= 0.0.0"<br /># Patch Release Comparisons<br />When you want to specify a minor version up to the next major version you<br />can use the "~" character to perform patch comparisons. Some examples:<br />  - "~1.2.3" is equivalent to ">=1.2.3, <1.3.0"<br />  - "~1" and "~1.x" is equivalent to ">=1, <2"<br />  - "~2.3" is equivalent to ">=2.3, <2.4"<br />  - "~1.2.x" is equivalent to ">=1.2.0, <1.3.0"<br /># Major Release Comparisons<br />You can use the "^" character to make major release comparisons after a<br />stable 1.0.0 version is published. If there is no stable version published, // minor versions define the stability level. Some examples:<br />  - "^1.2.3" is equivalent to ">=1.2.3, <2.0.0"<br />  - "^1.2.x" is equivalent to ">=1.2.0, <2.0.0"<br />  - "^2.3" is equivalent to ">=2.3, <3"<br />  - "^2.x" is equivalent to ">=2.0.0, <3"<br />  - "^0.2.3" is equivalent to ">=0.2.3, <0.3.0"<br />  - "^0.2" is equivalent to ">=0.2.0, <0.3.0"<br />  - "^0.0.3" is equvalent to ">=0.0.3, <0.0.4"<br />  - "^0.0" is equivalent to ">=0.0.0, <0.1.0"<br />  - "^0" is equivalent to ">=0.0.0, <1.0.0"<br /># OR Comparisons<br />You can use the "\|\|" character to represent an OR operation in the version<br />range. Some examples:<br />  - ">=1.2.3, <2.0.0 \|\| >3.0.0"<br />  - "^0 \|\| ^3 \|\| ^5"<br />For more information on semver, please see https://semver.org/ |  | MaxLength: 64 <br /> |
| `channels` _string array_ | channels is an optional reference to a set of channels belonging to<br />the package specified in the packageName field.<br />A "channel" is a package-author-defined stream of updates for an extension.<br />Each channel in the list must follow the DNS subdomain standard<br />as defined in [RFC 1123]. It must contain only lowercase alphanumeric characters,<br />hyphens (-) or periods (.), start and end with an alphanumeric character,<br />and be no longer than 253 characters. No more than 256 channels can be specified.<br />When specified, it is used to constrain the set of installable bundles and<br />the automated upgrade path. This constraint is an AND operation with the<br />version field. For example:<br />  - Given channel is set to "foo"<br />  - Given version is set to ">=1.0.0, <1.5.0"<br />  - Only bundles that exist in channel "foo" AND satisfy the version range comparison will be considered installable<br />  - Automatic upgrades will be constrained to upgrade edges defined by the selected channel<br />When unspecified, upgrade edges across all channels will be used to identify valid automatic upgrade paths.<br />Some examples of valid values are:<br />  - 1.1.x<br />  - alpha<br />  - stable<br />  - stable-v1<br />  - v1-stable<br />  - dev-preview<br />  - preview<br />  - community<br />Some examples of invalid values are:<br />  - -some-channel<br />  - some-channel-<br />  - thisisareallylongchannelnamethatisgreaterthanthemaximumlength<br />  - original_40<br />  - --default-channel<br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxItems: 256 <br />items:MaxLength: 253 <br />items:XValidation: \{self.matches("^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$") channels entries must be valid DNS1123 subdomains    <nil>\} <br /> |
| `selector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | selector is an optional field that can be used<br />to filter the set of ClusterCatalogs used in the bundle<br />selection process.<br />When unspecified, all ClusterCatalogs will be used in<br />the bundle selection process. |  |  |
| `upgradeConstraintPolicy` _[UpgradeConstraintPolicy](#upgradeconstraintpolicy)_ | upgradeConstraintPolicy is an optional field that controls whether<br />the upgrade path(s) defined in the catalog are enforced for the package<br />referenced in the packageName field.<br />Allowed values are: "CatalogProvided" or "SelfCertified", or omitted.<br />When this field is set to "CatalogProvided", automatic upgrades will only occur<br />when upgrade constraints specified by the package author are met.<br />When this field is set to "SelfCertified", the upgrade constraints specified by<br />the package author are ignored. This allows for upgrades and downgrades to<br />any version of the package. This is considered a dangerous operation as it<br />can lead to unknown and potentially disastrous outcomes, such as data<br />loss. It is assumed that users have independently verified changes when<br />using this option.<br />When this field is omitted, the default value is "CatalogProvided". | CatalogProvided | Enum: [CatalogProvided SelfCertified] <br /> |


#### CatalogSource



CatalogSource is a discriminated union of possible sources for a Catalog.
CatalogSource contains the sourcing information for a Catalog



_Appears in:_
- [ClusterCatalogSpec](#clustercatalogspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[SourceType](#sourcetype)_ | type is a reference to the type of source the catalog is sourced from.<br />type is required.<br />The only allowed value is "Image".<br />When set to "Image", the ClusterCatalog content will be sourced from an OCI image.<br />When using an image source, the image field must be set and must be the only field defined for this type. |  | Enum: [Image] <br />Required: \{\} <br /> |
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
| `source` _[CatalogSource](#catalogsource)_ | source allows a user to define the source of a catalog.<br />A "catalog" contains information on content that can be installed on a cluster.<br />Providing a catalog source makes the contents of the catalog discoverable and usable by<br />other on-cluster components.<br />These on-cluster components may do a variety of things with this information, such as<br />presenting the content in a GUI dashboard or installing content from the catalog on the cluster.<br />The catalog source must contain catalog metadata in the File-Based Catalog (FBC) format.<br />For more information on FBC, see https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs.<br />source is a required field.<br />Below is a minimal example of a ClusterCatalogSpec that sources a catalog from an image:<br /> source:<br />   type: Image<br />   image:<br />     ref: quay.io/operatorhubio/catalog:latest |  | Required: \{\} <br /> |
| `priority` _integer_ | priority allows the user to define a priority for a ClusterCatalog.<br />priority is optional.<br />A ClusterCatalog's priority is used by clients as a tie-breaker between ClusterCatalogs that meet the client's requirements.<br />A higher number means higher priority.<br />It is up to clients to decide how to handle scenarios where multiple ClusterCatalogs with the same priority meet their requirements.<br />When deciding how to break the tie in this scenario, it is recommended that clients prompt their users for additional input.<br />When omitted, the default priority is 0 because that is the zero value of integers.<br />Negative numbers can be used to specify a priority lower than the default.<br />Positive numbers can be used to specify a priority higher than the default.<br />The lowest possible value is -2147483648.<br />The highest possible value is 2147483647. | 0 |  |
| `availabilityMode` _[AvailabilityMode](#availabilitymode)_ | availabilityMode allows users to define how the ClusterCatalog is made available to clients on the cluster.<br />availabilityMode is optional.<br />Allowed values are "Available" and "Unavailable" and omitted.<br />When omitted, the default value is "Available".<br />When set to "Available", the catalog contents will be unpacked and served over the catalog content HTTP server.<br />Setting the availabilityMode to "Available" tells clients that they should consider this ClusterCatalog<br />and its contents as usable.<br />When set to "Unavailable", the catalog contents will no longer be served over the catalog content HTTP server.<br />When set to this availabilityMode it should be interpreted the same as the ClusterCatalog not existing.<br />Setting the availabilityMode to "Unavailable" can be useful in scenarios where a user may not want<br />to delete the ClusterCatalog all together, but would still like it to be treated as if it doesn't exist. | Available | Enum: [Unavailable Available] <br /> |


#### ClusterCatalogStatus



ClusterCatalogStatus defines the observed state of ClusterCatalog



_Appears in:_
- [ClusterCatalog](#clustercatalog)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | conditions is a representation of the current state for this ClusterCatalog.<br />The current condition types are Serving and Progressing.<br />The Serving condition is used to represent whether or not the contents of the catalog is being served via the HTTP(S) web server.<br />When it has a status of True and a reason of Available, the contents of the catalog are being served.<br />When it has a status of False and a reason of Unavailable, the contents of the catalog are not being served because the contents are not yet available.<br />When it has a status of False and a reason of UserSpecifiedUnavailable, the contents of the catalog are not being served because the catalog has been intentionally marked as unavailable.<br />The Progressing condition is used to represent whether or not the ClusterCatalog is progressing or is ready to progress towards a new state.<br />When it has a status of True and a reason of Retrying, there was an error in the progression of the ClusterCatalog that may be resolved on subsequent reconciliation attempts.<br />When it has a status of True and a reason of Succeeded, the ClusterCatalog has successfully progressed to a new state and is ready to continue progressing.<br />When it has a status of False and a reason of Blocked, there was an error in the progression of the ClusterCatalog that requires manual intervention for recovery.<br />In the case that the Serving condition is True with reason Available and Progressing is True with reason Retrying, the previously fetched<br />catalog contents are still being served via the HTTP(S) web server while we are progressing towards serving a new version of the catalog<br />contents. This could occur when we've initially fetched the latest contents from the source for this catalog and when polling for changes<br />to the contents we identify that there are updates to the contents. |  |  |
| `resolvedSource` _[ResolvedCatalogSource](#resolvedcatalogsource)_ | resolvedSource contains information about the resolved source based on the source type. |  |  |
| `urls` _[ClusterCatalogURLs](#clustercatalogurls)_ | urls contains the URLs that can be used to access the catalog. |  |  |
| `lastUnpacked` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#time-v1-meta)_ | lastUnpacked represents the last time the contents of the<br />catalog were extracted from their source format. As an example,<br />when using an Image source, the OCI image will be pulled and the<br />image layers written to a file-system backed cache. We refer to the<br />act of this extraction from the source format as "unpacking". |  |  |


#### ClusterCatalogURLs



ClusterCatalogURLs contains the URLs that can be used to access the catalog.



_Appears in:_
- [ClusterCatalogStatus](#clustercatalogstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `base` _string_ | base is a cluster-internal URL that provides endpoints for<br />accessing the content of the catalog.<br />It is expected that clients append the path for the endpoint they wish<br />to access.<br />Currently, only a single endpoint is served and is accessible at the path<br />/api/v1.<br />The endpoints served for the v1 API are:<br />  - /all - this endpoint returns the entirety of the catalog contents in the FBC format<br />As the needs of users and clients of the evolve, new endpoints may be added. |  | MaxLength: 525 <br />Required: \{\} <br /> |


#### ClusterExtension



ClusterExtension is the Schema for the clusterextensions API



_Appears in:_
- [ClusterExtensionList](#clusterextensionlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `olm.operatorframework.io/v1` | | |
| `kind` _string_ | `ClusterExtension` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ClusterExtensionSpec](#clusterextensionspec)_ | spec is an optional field that defines the desired state of the ClusterExtension. |  |  |
| `status` _[ClusterExtensionStatus](#clusterextensionstatus)_ | status is an optional field that defines the observed state of the ClusterExtension. |  |  |


#### ClusterExtensionConfig



ClusterExtensionConfig is a discriminated union which selects the source configuration values to be merged into
the ClusterExtension's rendered manifests.



_Appears in:_
- [ClusterExtensionSpec](#clusterextensionspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `configType` _[ClusterExtensionConfigType](#clusterextensionconfigtype)_ | configType is a required reference to the type of configuration source.<br />Allowed values are "Inline"<br />When this field is set to "Inline", the cluster extension configuration is defined inline within the<br />ClusterExtension resource. |  | Enum: [Inline] <br />Required: \{\} <br /> |
| `inline` _[JSON](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#json-v1-apiextensions-k8s-io)_ | inline contains JSON or YAML values specified directly in the<br />ClusterExtension.<br />inline must be set if configType is 'Inline'.<br />inline accepts arbitrary JSON/YAML objects.<br />inline is validation at runtime against the schema provided by the bundle if a schema is provided. |  | Type: object <br /> |


#### ClusterExtensionConfigType

_Underlying type:_ _string_





_Appears in:_
- [ClusterExtensionConfig](#clusterextensionconfig)

| Field | Description |
| --- | --- |
| `Inline` |  |


#### ClusterExtensionInstallConfig



ClusterExtensionInstallConfig is a union which selects the clusterExtension installation config.
ClusterExtensionInstallConfig requires the namespace and serviceAccount which should be used for the installation of packages.



_Appears in:_
- [ClusterExtensionSpec](#clusterextensionspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `preflight` _[PreflightConfig](#preflightconfig)_ | preflight is an optional field that can be used to configure the checks that are<br />run before installation or upgrade of the content for the package specified in the packageName field.<br />When specified, it replaces the default preflight configuration for install/upgrade actions.<br />When not specified, the default configuration will be used. |  |  |


#### ClusterExtensionInstallStatus



ClusterExtensionInstallStatus is a representation of the status of the identified bundle.



_Appears in:_
- [ClusterExtensionStatus](#clusterextensionstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bundle` _[BundleMetadata](#bundlemetadata)_ | bundle is a required field which represents the identifying attributes of a bundle.<br />A "bundle" is a versioned set of content that represents the resources that<br />need to be applied to a cluster to install a package. |  | Required: \{\} <br /> |


#### ClusterExtensionList



ClusterExtensionList contains a list of ClusterExtension





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `olm.operatorframework.io/v1` | | |
| `kind` _string_ | `ClusterExtensionList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ClusterExtension](#clusterextension) array_ | items is a required list of ClusterExtension objects. |  | Required: \{\} <br /> |


#### ClusterExtensionSpec



ClusterExtensionSpec defines the desired state of ClusterExtension



_Appears in:_
- [ClusterExtension](#clusterextension)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespace` _string_ | namespace is a reference to a Kubernetes namespace.<br />This is the namespace in which the provided ServiceAccount must exist.<br />It also designates the default namespace where namespace-scoped resources<br />for the extension are applied to the cluster.<br />Some extensions may contain namespace-scoped resources to be applied in other namespaces.<br />This namespace must exist.<br />namespace is required, immutable, and follows the DNS label standard<br />as defined in [RFC 1123]. It must contain only lowercase alphanumeric characters or hyphens (-),<br />start and end with an alphanumeric character, and be no longer than 63 characters<br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxLength: 63 <br />Required: \{\} <br /> |
| `serviceAccount` _[ServiceAccountReference](#serviceaccountreference)_ | serviceAccount is a reference to a ServiceAccount used to perform all interactions<br />with the cluster that are required to manage the extension.<br />The ServiceAccount must be configured with the necessary permissions to perform these interactions.<br />The ServiceAccount must exist in the namespace referenced in the spec.<br />serviceAccount is required. |  | Required: \{\} <br /> |
| `source` _[SourceConfig](#sourceconfig)_ | source is a required field which selects the installation source of content<br />for this ClusterExtension. Selection is performed by setting the sourceType.<br />Catalog is currently the only implemented sourceType, and setting the<br />sourcetype to "Catalog" requires the catalog field to also be defined.<br />Below is a minimal example of a source definition (in yaml):<br />source:<br />  sourceType: Catalog<br />  catalog:<br />    packageName: example-package |  | Required: \{\} <br /> |
| `install` _[ClusterExtensionInstallConfig](#clusterextensioninstallconfig)_ | install is an optional field used to configure the installation options<br />for the ClusterExtension such as the pre-flight check configuration. |  |  |
| `config` _[ClusterExtensionConfig](#clusterextensionconfig)_ | config is an optional field used to specify bundle specific configuration<br />used to configure the bundle. Configuration is bundle specific and a bundle may provide<br />a configuration schema. When not specified, the default configuration of the resolved bundle will be used.<br />config is validated against a configuration schema provided by the resolved bundle. If the bundle does not provide<br />a configuration schema the final manifests will be derived on a best-effort basis. More information on how<br />to configure the bundle should be found in its end-user documentation.<br /><opcon:experimental> |  |  |


#### ClusterExtensionStatus



ClusterExtensionStatus defines the observed state of a ClusterExtension.



_Appears in:_
- [ClusterExtension](#clusterextension)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | The set of condition types which apply to all spec.source variations are Installed and Progressing.<br />The Installed condition represents whether or not the bundle has been installed for this ClusterExtension.<br />When Installed is True and the Reason is Succeeded, the bundle has been successfully installed.<br />When Installed is False and the Reason is Failed, the bundle has failed to install.<br />The Progressing condition represents whether or not the ClusterExtension is advancing towards a new state.<br />When Progressing is True and the Reason is Succeeded, the ClusterExtension is making progress towards a new state.<br />When Progressing is True and the Reason is Retrying, the ClusterExtension has encountered an error that could be resolved on subsequent reconciliation attempts.<br />When Progressing is False and the Reason is Blocked, the ClusterExtension has encountered an error that requires manual intervention for recovery.<br />When the ClusterExtension is sourced from a catalog, if may also communicate a deprecation condition.<br />These are indications from a package owner to guide users away from a particular package, channel, or bundle.<br />BundleDeprecated is set if the requested bundle version is marked deprecated in the catalog.<br />ChannelDeprecated is set if the requested channel is marked deprecated in the catalog.<br />PackageDeprecated is set if the requested package is marked deprecated in the catalog.<br />Deprecated is a rollup condition that is present when any of the deprecated conditions are present. |  |  |
| `install` _[ClusterExtensionInstallStatus](#clusterextensioninstallstatus)_ | install is a representation of the current installation status for this ClusterExtension. |  |  |




#### ImageSource



ImageSource enables users to define the information required for sourcing a Catalog from an OCI image

If we see that there is a possibly valid digest-based image reference AND pollIntervalMinutes is specified,
reject the resource since there is no use in polling a digest-based image reference.



_Appears in:_
- [CatalogSource](#catalogsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ref` _string_ | ref allows users to define the reference to a container image containing Catalog contents.<br />ref is required.<br />ref can not be more than 1000 characters.<br />A reference can be broken down into 3 parts - the domain, name, and identifier.<br />The domain is typically the registry where an image is located.<br />It must be alphanumeric characters (lowercase and uppercase) separated by the "." character.<br />Hyphenation is allowed, but the domain must start and end with alphanumeric characters.<br />Specifying a port to use is also allowed by adding the ":" character followed by numeric values.<br />The port must be the last value in the domain.<br />Some examples of valid domain values are "registry.mydomain.io", "quay.io", "my-registry.io:8080".<br />The name is typically the repository in the registry where an image is located.<br />It must contain lowercase alphanumeric characters separated only by the ".", "_", "__", "-" characters.<br />Multiple names can be concatenated with the "/" character.<br />The domain and name are combined using the "/" character.<br />Some examples of valid name values are "operatorhubio/catalog", "catalog", "my-catalog.prod".<br />An example of the domain and name parts of a reference being combined is "quay.io/operatorhubio/catalog".<br />The identifier is typically the tag or digest for an image reference and is present at the end of the reference.<br />It starts with a separator character used to distinguish the end of the name and beginning of the identifier.<br />For a digest-based reference, the "@" character is the separator.<br />For a tag-based reference, the ":" character is the separator.<br />An identifier is required in the reference.<br />Digest-based references must contain an algorithm reference immediately after the "@" separator.<br />The algorithm reference must be followed by the ":" character and an encoded string.<br />The algorithm must start with an uppercase or lowercase alpha character followed by alphanumeric characters and may contain the "-", "_", "+", and "." characters.<br />Some examples of valid algorithm values are "sha256", "sha256+b64u", "multihash+base58".<br />The encoded string following the algorithm must be hex digits (a-f, A-F, 0-9) and must be a minimum of 32 characters.<br />Tag-based references must begin with a word character (alphanumeric + "_") followed by word characters or ".", and "-" characters.<br />The tag must not be longer than 127 characters.<br />An example of a valid digest-based image reference is "quay.io/operatorhubio/catalog@sha256:200d4ddb2a73594b91358fe6397424e975205bfbe44614f5846033cad64b3f05"<br />An example of a valid tag-based image reference is "quay.io/operatorhubio/catalog:latest" |  | MaxLength: 1000 <br />Required: \{\} <br /> |
| `pollIntervalMinutes` _integer_ | pollIntervalMinutes allows the user to set the interval, in minutes, at which the image source should be polled for new content.<br />pollIntervalMinutes is optional.<br />pollIntervalMinutes can not be specified when ref is a digest-based reference.<br />When omitted, the image will not be polled for new content. |  | Minimum: 1 <br /> |


#### PreflightConfig



PreflightConfig holds the configuration for the preflight checks.  If used, at least one preflight check must be non-nil.



_Appears in:_
- [ClusterExtensionInstallConfig](#clusterextensioninstallconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `crdUpgradeSafety` _[CRDUpgradeSafetyPreflightConfig](#crdupgradesafetypreflightconfig)_ | crdUpgradeSafety is used to configure the CRD Upgrade Safety pre-flight<br />checks that run prior to upgrades of installed content.<br />The CRD Upgrade Safety pre-flight check safeguards from unintended<br />consequences of upgrading a CRD, such as data loss. |  |  |


#### ResolvedCatalogSource



ResolvedCatalogSource is a discriminated union of resolution information for a Catalog.
ResolvedCatalogSource contains the information about a sourced Catalog



_Appears in:_
- [ClusterCatalogStatus](#clustercatalogstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[SourceType](#sourcetype)_ | type is a reference to the type of source the catalog is sourced from.<br />type is required.<br />The only allowed value is "Image".<br />When set to "Image", information about the resolved image source will be set in the 'image' field. |  | Enum: [Image] <br />Required: \{\} <br /> |
| `image` _[ResolvedImageSource](#resolvedimagesource)_ | image is a field containing resolution information for a catalog sourced from an image.<br />This field must be set when type is Image, and forbidden otherwise. |  |  |


#### ResolvedImageSource



ResolvedImageSource provides information about the resolved source of a Catalog sourced from an image.



_Appears in:_
- [ResolvedCatalogSource](#resolvedcatalogsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ref` _string_ | ref contains the resolved image digest-based reference.<br />The digest format is used so users can use other tooling to fetch the exact<br />OCI manifests that were used to extract the catalog contents. |  | MaxLength: 1000 <br />Required: \{\} <br /> |


#### ServiceAccountReference



ServiceAccountReference identifies the serviceAccount used fo install a ClusterExtension.



_Appears in:_
- [ClusterExtensionSpec](#clusterextensionspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is a required, immutable reference to the name of the ServiceAccount<br />to be used for installation and management of the content for the package<br />specified in the packageName field.<br />This ServiceAccount must exist in the installNamespace.<br />name follows the DNS subdomain standard as defined in [RFC 1123].<br />It must contain only lowercase alphanumeric characters,<br />hyphens (-) or periods (.), start and end with an alphanumeric character,<br />and be no longer than 253 characters.<br />Some examples of valid values are:<br />  - some-serviceaccount<br />  - 123-serviceaccount<br />  - 1-serviceaccount-2<br />  - someserviceaccount<br />  - some.serviceaccount<br />Some examples of invalid values are:<br />  - -some-serviceaccount<br />  - some-serviceaccount-<br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxLength: 253 <br />Required: \{\} <br /> |


#### SourceConfig



SourceConfig is a discriminated union which selects the installation source.



_Appears in:_
- [ClusterExtensionSpec](#clusterextensionspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `sourceType` _string_ | sourceType is a required reference to the type of install source.<br />Allowed values are "Catalog"<br />When this field is set to "Catalog", information for determining the<br />appropriate bundle of content to install will be fetched from<br />ClusterCatalog resources existing on the cluster.<br />When using the Catalog sourceType, the catalog field must also be set. |  | Enum: [Catalog] <br />Required: \{\} <br /> |
| `catalog` _[CatalogFilter](#catalogfilter)_ | catalog is used to configure how information is sourced from a catalog.<br />This field is required when sourceType is "Catalog", and forbidden otherwise. |  |  |


#### SourceType

_Underlying type:_ _string_

SourceType defines the type of source used for catalogs.



_Appears in:_
- [CatalogSource](#catalogsource)
- [ResolvedCatalogSource](#resolvedcatalogsource)

| Field | Description |
| --- | --- |
| `Image` |  |


#### UpgradeConstraintPolicy

_Underlying type:_ _string_





_Appears in:_
- [CatalogFilter](#catalogfilter)

| Field | Description |
| --- | --- |
| `CatalogProvided` | The extension will only upgrade if the new version satisfies<br />the upgrade constraints set by the package author.<br /> |
| `SelfCertified` | Unsafe option which allows an extension to be<br />upgraded or downgraded to any available version of the package and<br />ignore the upgrade path designed by package authors.<br />This assumes that users independently verify the outcome of the changes.<br />Use with caution as this can lead to unknown and potentially<br />disastrous results such as data loss.<br /> |


