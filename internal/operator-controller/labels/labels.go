package labels

const (
	// OwnerKindKey is the label key used to record the kind of the owner
	// resource responsible for creating or managing a ClusterExtensionRevision.
	OwnerKindKey = "olm.operatorframework.io/owner-kind"

	// OwnerNameKey is the label key used to record the name of the owner
	// resource responsible for creating or managing a ClusterExtensionRevision.
	OwnerNameKey = "olm.operatorframework.io/owner-name"

	// PackageNameKey is the label key used to record the package name
	// associated with a ClusterExtensionRevision.
	PackageNameKey = "olm.operatorframework.io/package-name"

	// BundleNameKey is the label key used to record the bundle name
	// associated with a ClusterExtensionRevision.
	BundleNameKey = "olm.operatorframework.io/bundle-name"

	// BundleVersionKey is the label key used to record the bundle version
	// associated with a ClusterExtensionRevision.
	BundleVersionKey = "olm.operatorframework.io/bundle-version"

	// BundleReferenceKey is the label key used to record an external reference
	// (such as an image or catalog reference) to the bundle for a
	// ClusterExtensionRevision.
	BundleReferenceKey = "olm.operatorframework.io/bundle-reference"

	// ServiceAccountNameKey is the annotation key used to record the name of
	// the ServiceAccount configured on the owning ClusterExtension. It is
	// applied as an annotation on ClusterExtensionRevision resources to
	// capture which ServiceAccount was used for their lifecycle operations.
	ServiceAccountNameKey = "olm.operatorframework.io/service-account-name"

	// ServiceAccountNamespaceKey is the annotation key used to record the
	// namespace of the ServiceAccount configured on the owning
	// ClusterExtension. It is applied as an annotation on
	// ClusterExtensionRevision resources together with ServiceAccountNameKey
	// so that the effective ServiceAccount identity used for
	// ClusterExtensionRevision operations is preserved.
	ServiceAccountNamespaceKey = "olm.operatorframework.io/service-account-namespace"
)
