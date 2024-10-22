# API Reference

## Packages
- [olm.operatorframework.io/v1alpha1](#olmoperatorframeworkiov1alpha1)


## olm.operatorframework.io/v1alpha1

Package v1alpha1 contains API Schema definitions for the olm v1alpha1 API group

### Resource Types
- [ClusterExtension](#clusterextension)
- [ClusterExtensionList](#clusterextensionlist)



#### BundleMetadata



BundleMetadata is a representation of the identifying attributes of a bundle.



_Appears in:_
- [ClusterExtensionInstallStatus](#clusterextensioninstallstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is required and follows the DNS subdomain standard<br />as defined in [RFC 1123]. It must contain only lowercase alphanumeric characters,<br />hyphens (-) or periods (.), start and end with an alphanumeric character,<br />and be no longer than 253 characters. |  | Required: \{\} <br /> |
| `version` _string_ | version is a required field and is a reference to the version that this bundle represents<br />version follows the semantic versioning standard as defined in https://semver.org/, but permits a leading 'v' character. |  | Required: \{\} <br /> |


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
| `enforcement` _[CRDUpgradeSafetyEnforcement](#crdupgradesafetyenforcement)_ | enforcement is a required field, used to configure the state of the CRD Upgrade Safety pre-flight check.<br /><br />Allowed values are "None" or "Strict". The default value is "Strict".<br /><br />When set to "None", the CRD Upgrade Safety pre-flight check will be skipped<br />when performing an upgrade operation. This should be used with caution as<br />unintended consequences such as data loss can occur.<br /><br />When set to "Strict", the CRD Upgrade Safety pre-flight check will be run when<br />performing an upgrade operation. | Strict | Enum: [None Strict] <br />Required: \{\} <br /> |


#### CatalogSource



CatalogSource defines the attributes used to identify and filter content from a catalog.



_Appears in:_
- [SourceConfig](#sourceconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `packageName` _string_ | packageName is a reference to the name of the package to be installed<br />and is used to filter the content from catalogs.<br /><br />packageName is required, immutable, and follows the DNS subdomain standard<br />as defined in [RFC 1123]. It must contain only lowercase alphanumeric characters,<br />hyphens (-) or periods (.), start and end with an alphanumeric character,<br />and be no longer than 253 characters.<br /><br />Some examples of valid values are:<br />  - some-package<br />  - 123-package<br />  - 1-package-2<br />  - somepackage<br /><br />Some examples of invalid values are:<br />  - -some-package<br />  - some-package-<br />  - thisisareallylongpackagenamethatisgreaterthanthemaximumlength<br />  - some.package<br /><br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxLength: 253 <br />Required: \{\} <br /> |
| `version` _string_ | version is an optional semver constraint (a specific version or range of versions). When unspecified, the latest version available will be installed.<br /><br />Acceptable version ranges are no longer than 64 characters.<br />Version ranges are composed of comma- or space-delimited values and one or<br />more comparison operators, known as comparison strings. Additional<br />comparison strings can be added using the OR operator (\|\|).<br /><br /># Range Comparisons<br /><br />To specify a version range, you can use a comparison string like ">=3.0,<br /><3.6". When specifying a range, automatic updates will occur within that<br />range. The example comparison string means "install any version greater than<br />or equal to 3.0.0 but less than 3.6.0.". It also states intent that if any<br />upgrades are available within the version range after initial installation,<br />those upgrades should be automatically performed.<br /><br /># Pinned Versions<br /><br />To specify an exact version to install you can use a version range that<br />"pins" to a specific version. When pinning to a specific version, no<br />automatic updates will occur. An example of a pinned version range is<br />"0.6.0", which means "only install version 0.6.0 and never<br />upgrade from this version".<br /><br /># Basic Comparison Operators<br /><br />The basic comparison operators and their meanings are:<br />  - "=", equal (not aliased to an operator)<br />  - "!=", not equal<br />  - "<", less than<br />  - ">", greater than<br />  - ">=", greater than OR equal to<br />  - "<=", less than OR equal to<br /><br /># Wildcard Comparisons<br /><br />You can use the "x", "X", and "*" characters as wildcard characters in all<br />comparison operations. Some examples of using the wildcard characters:<br />  - "1.2.x", "1.2.X", and "1.2.*" is equivalent to ">=1.2.0, < 1.3.0"<br />  - ">= 1.2.x", ">= 1.2.X", and ">= 1.2.*" is equivalent to ">= 1.2.0"<br />  - "<= 2.x", "<= 2.X", and "<= 2.*" is equivalent to "< 3"<br />  - "x", "X", and "*" is equivalent to ">= 0.0.0"<br /><br /># Patch Release Comparisons<br /><br />When you want to specify a minor version up to the next major version you<br />can use the "~" character to perform patch comparisons. Some examples:<br />  - "~1.2.3" is equivalent to ">=1.2.3, <1.3.0"<br />  - "~1" and "~1.x" is equivalent to ">=1, <2"<br />  - "~2.3" is equivalent to ">=2.3, <2.4"<br />  - "~1.2.x" is equivalent to ">=1.2.0, <1.3.0"<br /><br /># Major Release Comparisons<br /><br />You can use the "^" character to make major release comparisons after a<br />stable 1.0.0 version is published. If there is no stable version published, // minor versions define the stability level. Some examples:<br />  - "^1.2.3" is equivalent to ">=1.2.3, <2.0.0"<br />  - "^1.2.x" is equivalent to ">=1.2.0, <2.0.0"<br />  - "^2.3" is equivalent to ">=2.3, <3"<br />  - "^2.x" is equivalent to ">=2.0.0, <3"<br />  - "^0.2.3" is equivalent to ">=0.2.3, <0.3.0"<br />  - "^0.2" is equivalent to ">=0.2.0, <0.3.0"<br />  - "^0.0.3" is equvalent to ">=0.0.3, <0.0.4"<br />  - "^0.0" is equivalent to ">=0.0.0, <0.1.0"<br />  - "^0" is equivalent to ">=0.0.0, <1.0.0"<br /><br /># OR Comparisons<br />You can use the "\|\|" character to represent an OR operation in the version<br />range. Some examples:<br />  - ">=1.2.3, <2.0.0 \|\| >3.0.0"<br />  - "^0 \|\| ^3 \|\| ^5"<br /><br />For more information on semver, please see https://semver.org/ |  | MaxLength: 64 <br /> |
| `channels` _string array_ | channels is an optional reference to a set of channels belonging to<br />the package specified in the packageName field.<br /><br />A "channel" is a package-author-defined stream of updates for an extension.<br /><br />Each channel in the list must follow the DNS subdomain standard<br />as defined in [RFC 1123]. It must contain only lowercase alphanumeric characters,<br />hyphens (-) or periods (.), start and end with an alphanumeric character,<br />and be no longer than 253 characters.<br /><br />When specified, it is used to constrain the set of installable bundles and<br />the automated upgrade path. This constraint is an AND operation with the<br />version field. For example:<br />  - Given channel is set to "foo"<br />  - Given version is set to ">=1.0.0, <1.5.0"<br />  - Only bundles that exist in channel "foo" AND satisfy the version range comparison will be considered installable<br />  - Automatic upgrades will be constrained to upgrade edges defined by the selected channel<br /><br />When unspecified, upgrade edges across all channels will be used to identify valid automatic upgrade paths.<br /><br />Some examples of valid values are:<br />  - 1.1.x<br />  - alpha<br />  - stable<br />  - stable-v1<br />  - v1-stable<br />  - dev-preview<br />  - preview<br />  - community<br /><br />Some examples of invalid values are:<br />  - -some-channel<br />  - some-channel-<br />  - thisisareallylongchannelnamethatisgreaterthanthemaximumlength<br />  - original_40<br />  - --default-channel<br /><br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxItems: 256 <br /> |
| `selector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | selector is an optional field that can be used<br />to filter the set of ClusterCatalogs used in the bundle<br />selection process.<br /><br />When unspecified, all ClusterCatalogs will be used in<br />the bundle selection process. |  |  |
| `upgradeConstraintPolicy` _[UpgradeConstraintPolicy](#upgradeconstraintpolicy)_ | upgradeConstraintPolicy is an optional field that controls whether<br />the upgrade path(s) defined in the catalog are enforced for the package<br />referenced in the packageName field.<br /><br />Allowed values are: "CatalogProvided" or "SelfCertified", or omitted.<br /><br />When this field is set to "CatalogProvided", automatic upgrades will only occur<br />when upgrade constraints specified by the package author are met.<br /><br />When this field is set to "SelfCertified", the upgrade constraints specified by<br />the package author are ignored. This allows for upgrades and downgrades to<br />any version of the package. This is considered a dangerous operation as it<br />can lead to unknown and potentially disastrous outcomes, such as data<br />loss. It is assumed that users have independently verified changes when<br />using this option.<br /><br />When this field is omitted, the default value is "CatalogProvided". | CatalogProvided | Enum: [CatalogProvided SelfCertified] <br /> |


#### ClusterExtension



ClusterExtension is the Schema for the clusterextensions API



_Appears in:_
- [ClusterExtensionList](#clusterextensionlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `olm.operatorframework.io/v1alpha1` | | |
| `kind` _string_ | `ClusterExtension` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ClusterExtensionSpec](#clusterextensionspec)_ | spec is an optional field that defines the desired state of the ClusterExtension. |  |  |
| `status` _[ClusterExtensionStatus](#clusterextensionstatus)_ | status is an optional field that defines the observed state of the ClusterExtension. |  |  |


#### ClusterExtensionInstallConfig



ClusterExtensionInstallConfig is a union which selects the clusterExtension installation config.
ClusterExtensionInstallConfig requires the namespace and serviceAccount which should be used for the installation of packages.



_Appears in:_
- [ClusterExtensionSpec](#clusterextensionspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespace` _string_ | namespace designates the kubernetes Namespace where bundle content<br />for the 'packageName' package field will be applied and the necessary<br />service account can be found.<br />The bundle may contain cluster-scoped resources or resources that are<br />applied to other Namespaces. This Namespace is expected to exist.<br /><br />namespace is required, immutable, and follows the DNS label standard<br />as defined in [RFC 1123]. It must contain only lowercase alphanumeric characters or hyphens (-),<br />start and end with an alphanumeric character, and be no longer than 63 characters<br /><br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxLength: 63 <br />Required: \{\} <br /> |
| `serviceAccount` _[ServiceAccountReference](#serviceaccountreference)_ | serviceAccount is a required reference to a ServiceAccount that exists<br />in the installNamespace which is used to install and<br />manage the content for the package specified in the packageName field.<br /><br />In order to successfully install and manage the content for the package,<br />the ServiceAccount provided via this field should be configured with the<br />appropriate permissions to perform the necessary operations on all the<br />resources that are included in the bundle of content being applied. |  | Required: \{\} <br /> |
| `preflight` _[PreflightConfig](#preflightconfig)_ | preflight is an optional field that can be used to configure the checks that are<br />run before installation or upgrade of the content for the package specified in the packageName field.<br /><br />When specified, it replaces the default preflight configuration for install/upgrade actions.<br />When not specified, the default configuration will be used. |  |  |


#### ClusterExtensionInstallStatus



ClusterExtensionInstallStatus is a representation of the status of the identified bundle.



_Appears in:_
- [ClusterExtensionStatus](#clusterextensionstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bundle` _[BundleMetadata](#bundlemetadata)_ | bundle is a required field which represents the identifying attributes of a bundle.<br /><br />A "bundle" is a versioned set of content that represents the resources that<br />need to be applied to a cluster to install a package. |  | Required: \{\} <br /> |


#### ClusterExtensionList



ClusterExtensionList contains a list of ClusterExtension





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `olm.operatorframework.io/v1alpha1` | | |
| `kind` _string_ | `ClusterExtensionList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ClusterExtension](#clusterextension) array_ |  |  | Required: \{\} <br /> |


#### ClusterExtensionSpec



ClusterExtensionSpec defines the desired state of ClusterExtension



_Appears in:_
- [ClusterExtension](#clusterextension)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `source` _[SourceConfig](#sourceconfig)_ | source is a required field which selects the installation source of content<br />for this ClusterExtension. Selection is performed by setting the sourceType.<br /><br />Catalog is currently the only implemented sourceType, and setting the<br />sourcetype to "Catalog" requires the catalog field to also be defined.<br /><br />Below is a minimal example of a source definition (in yaml):<br /><br />source:<br />  sourceType: Catalog<br />  catalog:<br />    packageName: example-package |  | Required: \{\} <br /> |
| `install` _[ClusterExtensionInstallConfig](#clusterextensioninstallconfig)_ | install is a required field used to configure the installation options<br />for the ClusterExtension such as the installation namespace,<br />the service account and the pre-flight check configuration.<br /><br />Below is a minimal example of an installation definition (in yaml):<br />install:<br />   namespace: example-namespace<br />   serviceAccount:<br />     name: example-sa |  | Required: \{\} <br /> |


#### ClusterExtensionStatus



ClusterExtensionStatus defines the observed state of a ClusterExtension.



_Appears in:_
- [ClusterExtension](#clusterextension)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | The set of condition types which apply to all spec.source variations are Installed and Progressing.<br /><br />The Installed condition represents whether or not the bundle has been installed for this ClusterExtension.<br />When Installed is True and the Reason is Succeeded, the bundle has been successfully installed.<br />When Installed is False and the Reason is Failed, the bundle has failed to install.<br /><br />The Progressing condition represents whether or not the ClusterExtension is advancing towards a new state.<br />When Progressing is True and the Reason is Succeeded, the ClusterExtension is making progress towards a new state.<br />When Progressing is False and the Reason is Blocked, the ClusterExtension has encountered an error that requires manual intervention for recovery.<br />When Progressing is False and the Reason is Retrying, the ClusterExtension has encountered an error that could be resolved on subsequent reconciliation attempts.<br /><br />When the ClusterExtension is sourced from a catalog, if may also communicate a deprecation condition.<br />These are indications from a package owner to guide users away from a particular package, channel, or bundle.<br />BundleDeprecated is set if the requested bundle version is marked deprecated in the catalog.<br />ChannelDeprecated is set if the requested channel is marked deprecated in the catalog.<br />PackageDeprecated is set if the requested package is marked deprecated in the catalog.<br />Deprecated is a rollup condition that is present when any of the deprecated conditions are present. |  |  |
| `install` _[ClusterExtensionInstallStatus](#clusterextensioninstallstatus)_ | install is a representation of the current installation status for this ClusterExtension. |  |  |


#### PreflightConfig



PreflightConfig holds the configuration for the preflight checks.  If used, at least one preflight check must be non-nil.



_Appears in:_
- [ClusterExtensionInstallConfig](#clusterextensioninstallconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `crdUpgradeSafety` _[CRDUpgradeSafetyPreflightConfig](#crdupgradesafetypreflightconfig)_ | crdUpgradeSafety is used to configure the CRD Upgrade Safety pre-flight<br />checks that run prior to upgrades of installed content.<br /><br />The CRD Upgrade Safety pre-flight check safeguards from unintended<br />consequences of upgrading a CRD, such as data loss. |  |  |


#### ServiceAccountReference



ServiceAccountReference identifies the serviceAccount used fo install a ClusterExtension.



_Appears in:_
- [ClusterExtensionInstallConfig](#clusterextensioninstallconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is a required, immutable reference to the name of the ServiceAccount<br />to be used for installation and management of the content for the package<br />specified in the packageName field.<br /><br />This ServiceAccount must exist in the installNamespace.<br /><br />name follows the DNS subdomain standard as defined in [RFC 1123].<br />It must contain only lowercase alphanumeric characters,<br />hyphens (-) or periods (.), start and end with an alphanumeric character,<br />and be no longer than 253 characters.<br /><br />Some examples of valid values are:<br />  - some-serviceaccount<br />  - 123-serviceaccount<br />  - 1-serviceaccount-2<br />  - someserviceaccount<br />  - some.serviceaccount<br /><br />Some examples of invalid values are:<br />  - -some-serviceaccount<br />  - some-serviceaccount-<br /><br />[RFC 1123]: https://tools.ietf.org/html/rfc1123 |  | MaxLength: 253 <br />Required: \{\} <br /> |


#### SourceConfig



SourceConfig is a discriminated union which selects the installation source.



_Appears in:_
- [ClusterExtensionSpec](#clusterextensionspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `sourceType` _string_ | sourceType is a required reference to the type of install source.<br /><br />Allowed values are "Catalog"<br /><br />When this field is set to "Catalog", information for determining the<br />appropriate bundle of content to install will be fetched from<br />ClusterCatalog resources existing on the cluster.<br />When using the Catalog sourceType, the catalog field must also be set. |  | Enum: [Catalog] <br />Required: \{\} <br /> |
| `catalog` _[CatalogSource](#catalogsource)_ | catalog is used to configure how information is sourced from a catalog.<br />This field is required when sourceType is "Catalog", and forbidden otherwise. |  |  |


#### UpgradeConstraintPolicy

_Underlying type:_ _string_





_Appears in:_
- [CatalogSource](#catalogsource)

| Field | Description |
| --- | --- |
| `CatalogProvided` | The extension will only upgrade if the new version satisfies<br />the upgrade constraints set by the package author.<br /> |
| `SelfCertified` | Unsafe option which allows an extension to be<br />upgraded or downgraded to any available version of the package and<br />ignore the upgrade path designed by package authors.<br />This assumes that users independently verify the outcome of the changes.<br />Use with caution as this can lead to unknown and potentially<br />disastrous results such as data loss.<br /> |


