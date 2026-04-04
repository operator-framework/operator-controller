package labels

import corev1 "k8s.io/api/core/v1"

const (
	// SecretTypeObjectData is the custom Secret type used for Secrets that store
	// externalized object content referenced by ClusterObjectSet ref entries.
	// It distinguishes OLM-managed ref Secrets from user-created Secrets.
	SecretTypeObjectData corev1.SecretType = "olm.operatorframework.io/object-data" //nolint:gosec // G101 false positive: this is a Kubernetes Secret type identifier, not a credential

	// OwnerKindKey is the label key used to record the kind of the owner
	// resource responsible for creating or managing a ClusterObjectSet.
	OwnerKindKey = "olm.operatorframework.io/owner-kind"

	// OwnerNameKey is the label key used to record the name of the owner
	// resource responsible for creating or managing a ClusterObjectSet.
	OwnerNameKey = "olm.operatorframework.io/owner-name"

	// PackageNameKey is the label key used to record the package name
	// associated with a ClusterObjectSet.
	PackageNameKey = "olm.operatorframework.io/package-name"

	// BundleNameKey is the label key used to record the bundle name
	// associated with a ClusterObjectSet.
	BundleNameKey = "olm.operatorframework.io/bundle-name"

	// BundleVersionKey is the label key used to record the bundle version
	// associated with a ClusterObjectSet.
	BundleVersionKey = "olm.operatorframework.io/bundle-version"

	// BundleReferenceKey is the label key used to record an external reference
	// (such as an image or catalog reference) to the bundle for a
	// ClusterObjectSet.
	BundleReferenceKey = "olm.operatorframework.io/bundle-reference"

	// ServiceAccountNameKey is the annotation key used to record the name of
	// the ServiceAccount configured on the owning ClusterExtension. It is
	// applied as an annotation on ClusterObjectSet resources to
	// capture which ServiceAccount was used for their lifecycle operations.
	ServiceAccountNameKey = "olm.operatorframework.io/service-account-name"

	// ServiceAccountNamespaceKey is the annotation key used to record the
	// namespace of the ServiceAccount configured on the owning
	// ClusterExtension. It is applied as an annotation on
	// ClusterObjectSet resources together with ServiceAccountNameKey
	// so that the effective ServiceAccount identity used for
	// ClusterObjectSet operations is preserved.
	ServiceAccountNamespaceKey = "olm.operatorframework.io/service-account-namespace"

	// RevisionNameKey is the label key used to record the name of the
	// ClusterObjectSet that owns or references a resource (e.g. a
	// ref Secret). It enables efficient listing of all resources associated
	// with a specific revision.
	RevisionNameKey = "olm.operatorframework.io/revision-name"

	// MigratedFromHelmKey is the label key used to mark ClusterObjectSets
	// that were created during migration from Helm releases. This label is used
	// to distinguish migrated revisions from those created by normal Boxcutter operation.
	MigratedFromHelmKey = "olm.operatorframework.io/migrated-from-helm"

	// SourceSpecHashKey is the annotation key used to record a SHA-256 fingerprint
	// of the resolution-relevant fields from the ClusterExtension spec at the time
	// the revision was resolved. This allows the controller to detect spec changes
	// (e.g. version constraint, channels, selector, upgradeConstraintPolicy)
	// even when the rolling-out version still satisfies the new constraint.
	SourceSpecHashKey = "olm.operatorframework.io/source-spec-hash"
)
