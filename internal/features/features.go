package features

import (
	"os"

	"github.com/spf13/pflag"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

const (
	// Add new feature gates constants (strings)
	// Ex: SomeFeature featuregate.Feature = "SomeFeature"

	ForceSemverUpgradeConstraints featuregate.Feature = "ForceSemverUpgradeConstraints"
	RegistryV1WebhookSupport      featuregate.Feature = "RegistryV1WebhookSupport"
)

var operatorControllerFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	// Add new feature gate definitions
	// Ex: SomeFeature: {...}

	ForceSemverUpgradeConstraints: {Default: false, PreRelease: featuregate.Alpha},
	RegistryV1WebhookSupport:      {Default: false, PreRelease: featuregate.Alpha},
}

var OperatorControllerFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	utilruntime.Must(OperatorControllerFeatureGate.Add(operatorControllerFeatureGates))
}

func InitializeFromCLIFlags(realFlagSet *pflag.FlagSet) {
	OperatorControllerFeatureGate.AddFlag(realFlagSet)

	tmpFlagSet := pflag.NewFlagSet("", pflag.ContinueOnError)
	OperatorControllerFeatureGate.AddFlag(tmpFlagSet)
	tmpFlagSet.ParseErrorsWhitelist = pflag.ParseErrorsWhitelist{UnknownFlags: true}

	// Register a help flag so that we "own" it and override any
	// automatic handling that might come by default
	tmpFlagSet.BoolP("help", "h", false, "help for registryv1-to-helm")
	// We don't care about this error here.
	// If this errors, we'll catch it, when `realFlagSet` is parsed.
	_ = tmpFlagSet.Parse(os.Args[1:])
}
