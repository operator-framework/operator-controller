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
| `name` _string_ | name is required and follows the DNS subdomain standard as defined in [RFC 1123].<br />It must contain only lowercase alphanumeric characters, hyphens (-) or periods (.),<br />start and end with an alphanumeric character, and be no longer than 253 characters. |  | Required: \{\} <br /> |
| `version` _string_ | version is required and references the version that this bundle represents.<br />It follows the semantic versioning standard as defined in https://semver.org/. |  | Required: \{\} <br /> |


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
| `enforcement` _[CRDUpgradeSafetyEnforcement](#crdupgradesafetyenforcement)_ | enforcement is required and configures the state of the CRD Upgrade Safety pre-flight check.<br />Allowed values are "None" or "Strict". The default value is "Strict".<br />When set to "None", the CRD Upgrade Safety pre-flight check is skipped during an upgrade operation.<br />Use this option with caution as unintended consequences such as data loss can occur.<br />When set to "Strict", the CRD Upgrade Safety pre-flight check runs during an upgrade operation. |  | Enum: [None Strict] <br />Required: \{\} <br /> |


#### CatalogFilter



CatalogFilter defines the attributes used to identify and filter content from a catalog.



_Appears in:_
- [SourceConfig](#sourceconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `packageName` _string_ | packageName specifies the name of the package to be installed and is used to filter<br />the content from catalogs.<br />It is required, immutable, and follows the DNS subdomain standard as defined in [RFC 1123].<br />It must contain only lowercase alphanumeric characters, hyphens (-) or periods (.),<br />start and end with an alphanumeric character, and be no longer than 253 characters.<br />Some examples of valid values are:<br />  - some-package<br />  - 123-package<br />  - 1-package-2<br />  - somepackage<br />Some examples of invalid values are:<br />  - -some-package<br />  - some-package-<br />  - thisisareallylongpackagenamethatisgreaterthanthemaximumlength<br />  - some.package<br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxLength: 253 <br />Required: \{\} <br /> |
| `version` _string_ | version is an optional semver constraint (a specific version or range of versions).<br />When unspecified, the latest version available is installed.<br />Acceptable version ranges are no longer than 64 characters.<br />Version ranges are composed of comma- or space-delimited values and one or more comparison operators,<br />known as comparison strings.<br />You can add additional comparison strings using the OR operator (\|\|).<br /># Range Comparisons<br />To specify a version range, you can use a comparison string like ">=3.0,<br /><3.6". When specifying a range, automatic updates will occur within that<br />range. The example comparison string means "install any version greater than<br />or equal to 3.0.0 but less than 3.6.0.". It also states intent that if any<br />upgrades are available within the version range after initial installation,<br />those upgrades should be automatically performed.<br /># Pinned Versions<br />To specify an exact version to install you can use a version range that<br />"pins" to a specific version. When pinning to a specific version, no<br />automatic updates will occur. An example of a pinned version range is<br />"0.6.0", which means "only install version 0.6.0 and never<br />upgrade from this version".<br /># Basic Comparison Operators<br />The basic comparison operators and their meanings are:<br />  - "=", equal (not aliased to an operator)<br />  - "!=", not equal<br />  - "<", less than<br />  - ">", greater than<br />  - ">=", greater than OR equal to<br />  - "<=", less than OR equal to<br /># Wildcard Comparisons<br />You can use the "x", "X", and "*" characters as wildcard characters in all<br />comparison operations. Some examples of using the wildcard characters:<br />  - "1.2.x", "1.2.X", and "1.2.*" is equivalent to ">=1.2.0, < 1.3.0"<br />  - ">= 1.2.x", ">= 1.2.X", and ">= 1.2.*" is equivalent to ">= 1.2.0"<br />  - "<= 2.x", "<= 2.X", and "<= 2.*" is equivalent to "< 3"<br />  - "x", "X", and "*" is equivalent to ">= 0.0.0"<br /># Patch Release Comparisons<br />When you want to specify a minor version up to the next major version you<br />can use the "~" character to perform patch comparisons. Some examples:<br />  - "~1.2.3" is equivalent to ">=1.2.3, <1.3.0"<br />  - "~1" and "~1.x" is equivalent to ">=1, <2"<br />  - "~2.3" is equivalent to ">=2.3, <2.4"<br />  - "~1.2.x" is equivalent to ">=1.2.0, <1.3.0"<br /># Major Release Comparisons<br />You can use the "^" character to make major release comparisons after a<br />stable 1.0.0 version is published. If there is no stable version published, // minor versions define the stability level. Some examples:<br />  - "^1.2.3" is equivalent to ">=1.2.3, <2.0.0"<br />  - "^1.2.x" is equivalent to ">=1.2.0, <2.0.0"<br />  - "^2.3" is equivalent to ">=2.3, <3"<br />  - "^2.x" is equivalent to ">=2.0.0, <3"<br />  - "^0.2.3" is equivalent to ">=0.2.3, <0.3.0"<br />  - "^0.2" is equivalent to ">=0.2.0, <0.3.0"<br />  - "^0.0.3" is equvalent to ">=0.0.3, <0.0.4"<br />  - "^0.0" is equivalent to ">=0.0.0, <0.1.0"<br />  - "^0" is equivalent to ">=0.0.0, <1.0.0"<br /># OR Comparisons<br />You can use the "\|\|" character to represent an OR operation in the version<br />range. Some examples:<br />  - ">=1.2.3, <2.0.0 \|\| >3.0.0"<br />  - "^0 \|\| ^3 \|\| ^5"<br />For more information on semver, please see https://semver.org/ |  | MaxLength: 64 <br /> |
| `channels` _string array_ | channels is optional and specifies a set of channels belonging to the package<br />specified in the packageName field.<br />A channel is a package-author-defined stream of updates for an extension.<br />Each channel in the list must follow the DNS subdomain standard as defined in [RFC 1123].<br />It must contain only lowercase alphanumeric characters, hyphens (-) or periods (.),<br />start and end with an alphanumeric character, and be no longer than 253 characters.<br />You can specify no more than 256 channels.<br />When specified, it constrains the set of installable bundles and the automated upgrade path.<br />This constraint is an AND operation with the version field. For example:<br />  - Given channel is set to "foo"<br />  - Given version is set to ">=1.0.0, <1.5.0"<br />  - Only bundles that exist in channel "foo" AND satisfy the version range comparison are considered installable<br />  - Automatic upgrades are constrained to upgrade edges defined by the selected channel<br />When unspecified, upgrade edges across all channels are used to identify valid automatic upgrade paths.<br />Some examples of valid values are:<br />  - 1.1.x<br />  - alpha<br />  - stable<br />  - stable-v1<br />  - v1-stable<br />  - dev-preview<br />  - preview<br />  - community<br />Some examples of invalid values are:<br />  - -some-channel<br />  - some-channel-<br />  - thisisareallylongchannelnamethatisgreaterthanthemaximumlength<br />  - original_40<br />  - --default-channel<br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxItems: 256 <br />items:MaxLength: 253 <br />items:XValidation: \{self.matches("^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$") channels entries must be valid DNS1123 subdomains    <nil>\} <br /> |
| `selector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | selector is optional and filters the set of ClusterCatalogs used in the bundle selection process.<br />When unspecified, all ClusterCatalogs are used in the bundle selection process. |  |  |
| `upgradeConstraintPolicy` _[UpgradeConstraintPolicy](#upgradeconstraintpolicy)_ | upgradeConstraintPolicy is optional and controls whether the upgrade paths defined in the catalog<br />are enforced for the package referenced in the packageName field.<br />Allowed values are "CatalogProvided", "SelfCertified", or omitted.<br />When set to "CatalogProvided", automatic upgrades only occur when upgrade constraints specified by the package<br />author are met.<br />When set to "SelfCertified", the upgrade constraints specified by the package author are ignored.<br />This allows upgrades and downgrades to any version of the package.<br />This is considered a dangerous operation as it can lead to unknown and potentially disastrous outcomes,<br />such as data loss.<br />Use this option only if you have independently verified the changes.<br />When omitted, the default value is "CatalogProvided". | CatalogProvided | Enum: [CatalogProvided SelfCertified] <br /> |


#### CatalogSource



CatalogSource is a discriminated union of possible sources for a Catalog.
CatalogSource contains the sourcing information for a Catalog



_Appears in:_
- [ClusterCatalogSpec](#clustercatalogspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[SourceType](#sourcetype)_ | type is a required field that specifies the type of source for the catalog.<br />The only allowed value is "Image".<br />When set to "Image", the ClusterCatalog content is sourced from an OCI image.<br />When using an image source, the image field must be set and must be the only field defined for this type. |  | Enum: [Image] <br />Required: \{\} <br /> |
| `image` _[ImageSource](#imagesource)_ | image configures how catalog contents are sourced from an OCI image.<br />It is required when type is Image, and forbidden otherwise. |  |  |


#### ClusterCatalog



ClusterCatalog makes File-Based Catalog (FBC) data available to your cluster.
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
| `spec` _[ClusterCatalogSpec](#clustercatalogspec)_ | spec is a required field that defines the desired state of the ClusterCatalog.<br />The controller ensures that the catalog is unpacked and served over the catalog content HTTP server. |  | Required: \{\} <br /> |
| `status` _[ClusterCatalogStatus](#clustercatalogstatus)_ | status contains the following information about the state of the ClusterCatalog:<br />  - Whether the catalog contents are being served via the catalog content HTTP server<br />  - Whether the ClusterCatalog is progressing to a new state<br />  - A reference to the source from which the catalog contents were retrieved |  |  |


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
| `source` _[CatalogSource](#catalogsource)_ | source is a required field that defines the source of a catalog.<br />A catalog contains information on content that can be installed on a cluster.<br />The catalog source makes catalog contents discoverable and usable by other on-cluster components.<br />These components can present the content in a GUI dashboard or install content from the catalog on the cluster.<br />The catalog source must contain catalog metadata in the File-Based Catalog (FBC) format.<br />For more information on FBC, see https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs.<br />Below is a minimal example of a ClusterCatalogSpec that sources a catalog from an image:<br /> source:<br />   type: Image<br />   image:<br />     ref: quay.io/operatorhubio/catalog:latest |  | Required: \{\} <br /> |
| `priority` _integer_ | priority is an optional field that defines a priority for this ClusterCatalog.<br />Clients use the ClusterCatalog priority as a tie-breaker between ClusterCatalogs that meet their requirements.<br />Higher numbers mean higher priority.<br />Clients decide how to handle scenarios where multiple ClusterCatalogs with the same priority meet their requirements.<br />Clients should prompt users for additional input to break the tie.<br />When omitted, the default priority is 0.<br />Use negative numbers to specify a priority lower than the default.<br />Use positive numbers to specify a priority higher than the default.<br />The lowest possible value is -2147483648.<br />The highest possible value is 2147483647. | 0 | Maximum: 2.147483647e+09 <br />Minimum: -2.147483648e+09 <br /> |
| `availabilityMode` _[AvailabilityMode](#availabilitymode)_ | availabilityMode is an optional field that defines how the ClusterCatalog is made available to clients on the cluster.<br />Allowed values are "Available", "Unavailable", or omitted.<br />When omitted, the default value is "Available".<br />When set to "Available", the catalog contents are unpacked and served over the catalog content HTTP server.<br />Clients should consider this ClusterCatalog and its contents as usable.<br />When set to "Unavailable", the catalog contents are no longer served over the catalog content HTTP server.<br />Treat this the same as if the ClusterCatalog does not exist.<br />Use "Unavailable" when you want to keep the ClusterCatalog but treat it as if it doesn't exist. | Available | Enum: [Unavailable Available] <br /> |


#### ClusterCatalogStatus



ClusterCatalogStatus defines the observed state of ClusterCatalog



_Appears in:_
- [ClusterCatalog](#clustercatalog)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | conditions represents the current state of this ClusterCatalog.<br />The current condition types are Serving and Progressing.<br />The Serving condition represents whether the catalog contents are being served via the HTTP(S) web server:<br />  - When status is True and reason is Available, the catalog contents are being served.<br />  - When status is False and reason is Unavailable, the catalog contents are not being served because the contents are not yet available.<br />  - When status is False and reason is UserSpecifiedUnavailable, the catalog contents are not being served because the catalog has been intentionally marked as unavailable.<br />The Progressing condition represents whether the ClusterCatalog is progressing or is ready to progress towards a new state:<br />  - When status is True and reason is Retrying, an error occurred that may be resolved on subsequent reconciliation attempts.<br />  - When status is True and reason is Succeeded, the ClusterCatalog has successfully progressed to a new state and is ready to continue progressing.<br />  - When status is False and reason is Blocked, an error occurred that requires manual intervention for recovery.<br />If the system initially fetched contents and polling identifies updates, both conditions can be active simultaneously:<br />  - The Serving condition remains True with reason Available because the previous contents are still served via the HTTP(S) web server.<br />  - The Progressing condition is True with reason Retrying because the system is working to serve the new version. |  |  |
| `resolvedSource` _[ResolvedCatalogSource](#resolvedcatalogsource)_ | resolvedSource contains information about the resolved source based on the source type. |  |  |
| `urls` _[ClusterCatalogURLs](#clustercatalogurls)_ | urls contains the URLs that can be used to access the catalog. |  |  |
| `lastUnpacked` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#time-v1-meta)_ | lastUnpacked represents the last time the catalog contents were extracted from their source format.<br />For example, when using an Image source, the OCI image is pulled and image layers are written to a file-system backed cache.<br />This extraction from the source format is called "unpacking". |  |  |


#### ClusterCatalogURLs



ClusterCatalogURLs contains the URLs that can be used to access the catalog.



_Appears in:_
- [ClusterCatalogStatus](#clustercatalogstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `base` _string_ | base is a cluster-internal URL that provides endpoints for accessing the catalog content.<br />Clients should append the path for the endpoint they want to access.<br />Currently, only a single endpoint is served and is accessible at the path /api/v1.<br />The endpoints served for the v1 API are:<br />  - /all - this endpoint returns the entire catalog contents in the FBC format<br />New endpoints may be added as needs evolve. |  | MaxLength: 525 <br />Required: \{\} <br /> |


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
| `configType` _[ClusterExtensionConfigType](#clusterextensionconfigtype)_ | configType is required and specifies the type of configuration source.<br />The only allowed value is "Inline".<br />When set to "Inline", the cluster extension configuration is defined inline within the ClusterExtension resource. |  | Enum: [Inline] <br />Required: \{\} <br /> |
| `inline` _[JSON](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#json-v1-apiextensions-k8s-io)_ | inline contains JSON or YAML values specified directly in the ClusterExtension.<br />It is used to specify arbitrary configuration values for the ClusterExtension.<br />It must be set if configType is 'Inline' and must be a valid JSON/YAML object containing at least one property.<br />The configuration values are validated at runtime against a JSON schema provided by the bundle. |  | MinProperties: 1 <br />Type: object <br /> |


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
| `preflight` _[PreflightConfig](#preflightconfig)_ | preflight is optional and configures the checks that run before installation or upgrade<br />of the content for the package specified in the packageName field.<br />When specified, it replaces the default preflight configuration for install/upgrade actions.<br />When not specified, the default configuration is used. |  |  |


#### ClusterExtensionInstallStatus



ClusterExtensionInstallStatus is a representation of the status of the identified bundle.



_Appears in:_
- [ClusterExtensionStatus](#clusterextensionstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bundle` _[BundleMetadata](#bundlemetadata)_ | bundle is required and represents the identifying attributes of a bundle.<br />A "bundle" is a versioned set of content that represents the resources that need to be applied<br />to a cluster to install a package. |  | Required: \{\} <br /> |


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
| `namespace` _string_ | namespace specifies a Kubernetes namespace.<br />This is the namespace where the provided ServiceAccount must exist.<br />It also designates the default namespace where namespace-scoped resources for the extension are applied to the cluster.<br />Some extensions may contain namespace-scoped resources to be applied in other namespaces.<br />This namespace must exist.<br />The namespace field is required, immutable, and follows the DNS label standard as defined in [RFC 1123].<br />It must contain only lowercase alphanumeric characters or hyphens (-), start and end with an alphanumeric character,<br />and be no longer than 63 characters.<br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxLength: 63 <br />Required: \{\} <br /> |
| `serviceAccount` _[ServiceAccountReference](#serviceaccountreference)_ | serviceAccount specifies a ServiceAccount used to perform all interactions with the cluster<br />that are required to manage the extension.<br />The ServiceAccount must be configured with the necessary permissions to perform these interactions.<br />The ServiceAccount must exist in the namespace referenced in the spec.<br />The serviceAccount field is required. |  | Required: \{\} <br /> |
| `source` _[SourceConfig](#sourceconfig)_ | source is required and selects the installation source of content for this ClusterExtension.<br />Set the sourceType field to perform the selection.<br />Catalog is currently the only implemented sourceType.<br />Setting sourceType to "Catalog" requires the catalog field to also be defined.<br />Below is a minimal example of a source definition (in yaml):<br />source:<br />  sourceType: Catalog<br />  catalog:<br />    packageName: example-package |  | Required: \{\} <br /> |
| `install` _[ClusterExtensionInstallConfig](#clusterextensioninstallconfig)_ | install is optional and configures installation options for the ClusterExtension,<br />such as the pre-flight check configuration. |  |  |
| `config` _[ClusterExtensionConfig](#clusterextensionconfig)_ | config is optional and specifies bundle-specific configuration.<br />Configuration is bundle-specific and a bundle may provide a configuration schema.<br />When not specified, the default configuration of the resolved bundle is used.<br />config is validated against a configuration schema provided by the resolved bundle. If the bundle does not provide<br />a configuration schema the bundle is deemed to not be configurable. More information on how<br />to configure bundles can be found in the OLM documentation associated with your current OLM version. |  |  |


#### ClusterExtensionStatus



ClusterExtensionStatus defines the observed state of a ClusterExtension.



_Appears in:_
- [ClusterExtension](#clusterextension)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | conditions represents the current state of the ClusterExtension.<br />The set of condition types which apply to all spec.source variations are Installed and Progressing.<br />The Installed condition represents whether the bundle has been installed for this ClusterExtension:<br />  - When Installed is True and the Reason is Succeeded, the bundle has been successfully installed.<br />  - When Installed is False and the Reason is Failed, the bundle has failed to install.<br />The Progressing condition represents whether or not the ClusterExtension is advancing towards a new state.<br />When Progressing is True and the Reason is Succeeded, the ClusterExtension is making progress towards a new state.<br />When Progressing is True and the Reason is Retrying, the ClusterExtension has encountered an error that could be resolved on subsequent reconciliation attempts.<br />When Progressing is False and the Reason is Blocked, the ClusterExtension has encountered an error that requires manual intervention for recovery.<br /><opcon:experimental:description><br />When Progressing is True and Reason is RollingOut, the ClusterExtension has one or more ClusterExtensionRevisions in active roll out.<br /></opcon:experimental:description><br />When the ClusterExtension is sourced from a catalog, it may also communicate a deprecation condition.<br />These are indications from a package owner to guide users away from a particular package, channel, or bundle:<br />  - BundleDeprecated is set if the requested bundle version is marked deprecated in the catalog.<br />  - ChannelDeprecated is set if the requested channel is marked deprecated in the catalog.<br />  - PackageDeprecated is set if the requested package is marked deprecated in the catalog.<br />  - Deprecated is a rollup condition that is present when any of the deprecated conditions are present. |  |  |
| `install` _[ClusterExtensionInstallStatus](#clusterextensioninstallstatus)_ | install is a representation of the current installation status for this ClusterExtension. |  |  |
| `activeRevisions` _[RevisionStatus](#revisionstatus) array_ | activeRevisions holds a list of currently active (non-archived) ClusterExtensionRevisions,<br />including both installed and rolling out revisions.<br /><opcon:experimental> |  |  |




#### ImageSource



ImageSource enables users to define the information required for sourcing a Catalog from an OCI image

If we see that there is a possibly valid digest-based image reference AND pollIntervalMinutes is specified,
reject the resource since there is no use in polling a digest-based image reference.



_Appears in:_
- [CatalogSource](#catalogsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ref` _string_ | ref is a required field that defines the reference to a container image containing catalog contents.<br />It cannot be more than 1000 characters.<br />A reference has 3 parts: the domain, name, and identifier.<br />The domain is typically the registry where an image is located.<br />It must be alphanumeric characters (lowercase and uppercase) separated by the "." character.<br />Hyphenation is allowed, but the domain must start and end with alphanumeric characters.<br />Specifying a port to use is also allowed by adding the ":" character followed by numeric values.<br />The port must be the last value in the domain.<br />Some examples of valid domain values are "registry.mydomain.io", "quay.io", "my-registry.io:8080".<br />The name is typically the repository in the registry where an image is located.<br />It must contain lowercase alphanumeric characters separated only by the ".", "_", "__", "-" characters.<br />Multiple names can be concatenated with the "/" character.<br />The domain and name are combined using the "/" character.<br />Some examples of valid name values are "operatorhubio/catalog", "catalog", "my-catalog.prod".<br />An example of the domain and name parts of a reference being combined is "quay.io/operatorhubio/catalog".<br />The identifier is typically the tag or digest for an image reference and is present at the end of the reference.<br />It starts with a separator character used to distinguish the end of the name and beginning of the identifier.<br />For a digest-based reference, the "@" character is the separator.<br />For a tag-based reference, the ":" character is the separator.<br />An identifier is required in the reference.<br />Digest-based references must contain an algorithm reference immediately after the "@" separator.<br />The algorithm reference must be followed by the ":" character and an encoded string.<br />The algorithm must start with an uppercase or lowercase alpha character followed by alphanumeric characters and may contain the "-", "_", "+", and "." characters.<br />Some examples of valid algorithm values are "sha256", "sha256+b64u", "multihash+base58".<br />The encoded string following the algorithm must be hex digits (a-f, A-F, 0-9) and must be a minimum of 32 characters.<br />Tag-based references must begin with a word character (alphanumeric + "_") followed by word characters or ".", and "-" characters.<br />The tag must not be longer than 127 characters.<br />An example of a valid digest-based image reference is "quay.io/operatorhubio/catalog@sha256:200d4ddb2a73594b91358fe6397424e975205bfbe44614f5846033cad64b3f05"<br />An example of a valid tag-based image reference is "quay.io/operatorhubio/catalog:latest" |  | MaxLength: 1000 <br />Required: \{\} <br /> |
| `pollIntervalMinutes` _integer_ | pollIntervalMinutes is an optional field that sets the interval, in minutes, at which the image source is polled for new content.<br />You cannot specify pollIntervalMinutes when ref is a digest-based reference.<br />When omitted, the image is not polled for new content. |  | Minimum: 1 <br /> |


#### PreflightConfig



PreflightConfig holds the configuration for the preflight checks.  If used, at least one preflight check must be non-nil.



_Appears in:_
- [ClusterExtensionInstallConfig](#clusterextensioninstallconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `crdUpgradeSafety` _[CRDUpgradeSafetyPreflightConfig](#crdupgradesafetypreflightconfig)_ | crdUpgradeSafety configures the CRD Upgrade Safety pre-flight checks that run<br />before upgrades of installed content.<br />The CRD Upgrade Safety pre-flight check safeguards from unintended consequences of upgrading a CRD,<br />such as data loss. |  |  |


#### ResolvedCatalogSource



ResolvedCatalogSource is a discriminated union of resolution information for a Catalog.
ResolvedCatalogSource contains the information about a sourced Catalog



_Appears in:_
- [ClusterCatalogStatus](#clustercatalogstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[SourceType](#sourcetype)_ | type is a required field that specifies the type of source for the catalog.<br />The only allowed value is "Image".<br />When set to "Image", information about the resolved image source is set in the image field. |  | Enum: [Image] <br />Required: \{\} <br /> |
| `image` _[ResolvedImageSource](#resolvedimagesource)_ | image contains resolution information for a catalog sourced from an image.<br />It must be set when type is Image, and forbidden otherwise. |  |  |


#### ResolvedImageSource



ResolvedImageSource provides information about the resolved source of a Catalog sourced from an image.



_Appears in:_
- [ResolvedCatalogSource](#resolvedcatalogsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ref` _string_ | ref contains the resolved image digest-based reference.<br />The digest format allows you to use other tooling to fetch the exact OCI manifests<br />that were used to extract the catalog contents. |  | MaxLength: 1000 <br />Required: \{\} <br /> |


#### RevisionStatus



RevisionStatus defines the observed state of a ClusterExtensionRevision.



_Appears in:_
- [ClusterExtensionStatus](#clusterextensionstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name of the ClusterExtensionRevision resource |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | conditions optionally expose Progressing and Available condition of the revision,<br />in case when it is not yet marked as successfully installed (condition Succeeded is not set to True).<br />Given that a ClusterExtension should remain available during upgrades, an observer may use these conditions<br />to get more insights about reasons for its current state. |  |  |


#### ServiceAccountReference



ServiceAccountReference identifies the serviceAccount used fo install a ClusterExtension.



_Appears in:_
- [ClusterExtensionSpec](#clusterextensionspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is a required, immutable reference to the name of the ServiceAccount used for installation<br />and management of the content for the package specified in the packageName field.<br />This ServiceAccount must exist in the installNamespace.<br />The name field follows the DNS subdomain standard as defined in [RFC 1123].<br />It must contain only lowercase alphanumeric characters, hyphens (-) or periods (.),<br />start and end with an alphanumeric character, and be no longer than 253 characters.<br />Some examples of valid values are:<br />  - some-serviceaccount<br />  - 123-serviceaccount<br />  - 1-serviceaccount-2<br />  - someserviceaccount<br />  - some.serviceaccount<br />Some examples of invalid values are:<br />  - -some-serviceaccount<br />  - some-serviceaccount-<br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxLength: 253 <br />Required: \{\} <br /> |


#### SourceConfig



SourceConfig is a discriminated union which selects the installation source.



_Appears in:_
- [ClusterExtensionSpec](#clusterextensionspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `sourceType` _string_ | sourceType is required and specifies the type of install source.<br />The only allowed value is "Catalog".<br />When set to "Catalog", information for determining the appropriate bundle of content to install<br />is fetched from ClusterCatalog resources on the cluster.<br />When using the Catalog sourceType, the catalog field must also be set. |  | Enum: [Catalog] <br />Required: \{\} <br /> |
| `catalog` _[CatalogFilter](#catalogfilter)_ | catalog configures how information is sourced from a catalog.<br />It is required when sourceType is "Catalog", and forbidden otherwise. |  |  |


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


