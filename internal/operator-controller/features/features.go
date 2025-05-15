package features

import (
	"github.com/go-logr/logr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"

	fgutil "github.com/operator-framework/operator-controller/internal/shared/util/featuregates"
)

const (
	// Add new feature gates constants (strings)
	// Ex: SomeFeature featuregate.Feature = "SomeFeature"
	PreflightPermissions              featuregate.Feature = "PreflightPermissions"
	SingleOwnNamespaceInstallSupport  featuregate.Feature = "SingleOwnNamespaceInstallSupport"
	SyntheticPermissions              featuregate.Feature = "SyntheticPermissions"
	WebhookProviderCertManager        featuregate.Feature = "WebhookProviderCertManager"
	WebhookProviderOpenshiftServiceCA featuregate.Feature = "WebhookProviderOpenshiftServiceCA"
)

var operatorControllerFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	// Add new feature gate definitions
	// Ex: SomeFeature: {...}
	PreflightPermissions: {
		Default:       false,
		PreRelease:    featuregate.Alpha,
		LockToDefault: false,
	},

	// SingleOwnNamespaceInstallSupport enables support for installing
	// registry+v1 cluster extensions with single or own namespaces modes
	// i.e. with a single watch namespace.
	SingleOwnNamespaceInstallSupport: {
		Default:       false,
		PreRelease:    featuregate.Alpha,
		LockToDefault: false,
	},

	// SyntheticPermissions enables support for a synthetic user permission
	// model to manage operator permission boundaries
	SyntheticPermissions: {
		Default:       false,
		PreRelease:    featuregate.Alpha,
		LockToDefault: false,
	},

	// WebhookProviderCertManager enables support for installing
	// registry+v1 cluster extensions that include validating,
	// mutating, and/or conversion webhooks with CertManager
	// as the certificate provider.
	WebhookProviderCertManager: {
		Default:       false,
		PreRelease:    featuregate.Alpha,
		LockToDefault: false,
	},

	// WebhookProviderCertManager enables support for installing
	// registry+v1 cluster extensions that include validating,
	// mutating, and/or conversion webhooks with Openshift Service CA
	// as the certificate provider.
	WebhookProviderOpenshiftServiceCA: {
		Default:       false,
		PreRelease:    featuregate.Alpha,
		LockToDefault: false,
	},
}

var OperatorControllerFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	utilruntime.Must(OperatorControllerFeatureGate.Add(operatorControllerFeatureGates))
}

// LogFeatureGateStates logs the state of all known feature gates.
func LogFeatureGateStates(log logr.Logger, fg featuregate.FeatureGate) {
	fgutil.LogFeatureGateStates(log, "feature gate status", fg, operatorControllerFeatureGates)
}
