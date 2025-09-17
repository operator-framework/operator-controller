package applier

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

// GetWatchNamespace determines the watch namespace the ClusterExtension should use
// Note: this is a temporary artifice to enable gated use of single/own namespace install modes
// for registry+v1 bundles. This will go away once the ClusterExtension API is updated to include
// (opaque) runtime configuration.
func GetWatchNamespace(ext *ocv1.ClusterExtension) (string, error) {
	if !features.OperatorControllerFeatureGate.Enabled(features.SingleOwnNamespaceInstallSupport) {
		return "", nil
	}

	var watchNamespace string
	if ext.Spec.Config != nil && ext.Spec.Config.Inline != nil {
		cfg := struct {
			WatchNamespace string `json:"watchNamespace"`
		}{}
		if err := json.Unmarshal(ext.Spec.Config.Inline.Raw, &cfg); err != nil {
			return "", fmt.Errorf("invalid bundle configuration: %w", err)
		}
		watchNamespace = cfg.WatchNamespace
	} else {
		return "", nil
	}

	if errs := validation.IsDNS1123Subdomain(watchNamespace); len(errs) > 0 {
		return "", fmt.Errorf("invalid watch namespace '%s': namespace must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character", watchNamespace)
	}

	return watchNamespace, nil
}
