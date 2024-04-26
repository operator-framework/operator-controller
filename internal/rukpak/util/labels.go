package util

const (
	OwnerKindKey = "olm.operatorframework.io/owner-kind"
	OwnerNameKey = "olm.operatorframework.io/owner-name"

	// Helm Secret annotations use the regex `(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?`
	// to validate labels. Open to any suggestions, keeping this for now.
	PackageNameKey   = "olm_operatorframework_io_package_name"
	BundleNameKey    = "olm_operatorframework_io_bundle_name"
	BundleVersionKey = "olm_operatorframework_io_bundle_version"
)
